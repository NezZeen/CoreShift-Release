package tunlayer

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func baseOptions() Options {
	return Options{
		StrictRoute: true,
		Upstream:    netip.MustParseAddrPort("127.0.0.1:17890"),
		DNS: DNSOptions{
			Remote: "https://1.1.1.1/dns-query",
			Direct: "192.168.1.1",
			FakeIP: true,
		},
	}
}

// render runs Build and decodes the JSON the way sing-box would see it.
func render(t *testing.T, o Options) map[string]any {
	t.Helper()
	b, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func list(m map[string]any, key string) []any { l, _ := m[key].([]any); return l }
func sub(m map[string]any, key string) map[string]any {
	s, _ := m[key].(map[string]any)
	return s
}

// find returns the first element of l whose fields include all of match.
func find(l []any, match map[string]any) map[string]any {
	for _, e := range l {
		m := e.(map[string]any)
		ok := true
		for k, v := range match {
			if !reflect.DeepEqual(m[k], v) {
				ok = false
				break
			}
		}
		if ok {
			return m
		}
	}
	return nil
}

func TestBuildDefaults(t *testing.T) {
	cfg := render(t, baseOptions())

	tun := list(cfg, "inbounds")[0].(map[string]any)
	if tun["type"] != "tun" || tun["interface_name"] != "coreshift" || tun["strict_route"] != true || tun["auto_route"] != true {
		t.Errorf("tun inbound = %v", tun)
	}
	if !reflect.DeepEqual(tun["address"], []any{"172.19.0.1/30"}) {
		t.Errorf("tun address = %v", tun["address"])
	}

	route := sub(cfg, "route")
	rules := list(route, "rules")
	if !reflect.DeepEqual(rules[0], map[string]any{"action": "sniff"}) ||
		!reflect.DeepEqual(rules[1], map[string]any{"protocol": "dns", "action": "hijack-dns"}) {
		t.Errorf("route must start with sniff + hijack-dns, got %v", rules[:2])
	}
	if route["final"] != "proxy" {
		t.Errorf("route.final = %v", route["final"])
	}

	dns := sub(cfg, "dns")
	if dns["final"] != "remote" || dns["strategy"] != "ipv4_only" {
		t.Errorf("dns final/strategy = %v/%v", dns["final"], dns["strategy"])
	}
	servers := list(dns, "servers")
	remote := find(servers, map[string]any{"tag": "remote"})
	if remote["type"] != "https" || remote["server"] != "1.1.1.1" || remote["detour"] != "proxy" || remote["path"] != nil {
		t.Errorf("remote server = %v", remote)
	}
	direct := find(servers, map[string]any{"tag": "direct"})
	if direct["type"] != "udp" || direct["server"] != "192.168.1.1" || direct["detour"] != nil {
		t.Errorf("direct server = %v", direct)
	}
	if find(servers, map[string]any{"type": "fakeip", "inet4_range": "198.18.0.0/15"}) == nil {
		t.Error("fakeip server missing")
	}
	if find(list(dns, "rules"), map[string]any{"server": "fakeip"}) == nil {
		t.Error("fakeip DNS rule missing")
	}

	socks := find(list(cfg, "outbounds"), map[string]any{"tag": "proxy"})
	if socks["type"] != "socks" || socks["server"] != "127.0.0.1" || socks["server_port"] != float64(17890) {
		t.Errorf("proxy outbound = %v", socks)
	}
}

// A remote connection that died silently is replaced only when a lookup on
// it times out: the timeout must be short enough that the system resolvers'
// retry (after 5 s) finds a fresh one, and set in every mode.
func TestDNSTimeout(t *testing.T) {
	selective := baseOptions()
	selective.Selective = true
	for name, o := range map[string]Options{"default": baseOptions(), "selective": selective} {
		raw, _ := sub(render(t, o), "dns")["timeout"].(string)
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 || d >= 5*time.Second {
			t.Errorf("%s: dns.timeout = %q, want a duration under 5s", name, raw)
		}
	}
}

func TestFakeIPNeverRoutedDirect(t *testing.T) {
	for _, r := range lanRanges {
		p := netip.MustParsePrefix(r)
		if p.Overlaps(DefaultFakeIPRange) || p.Overlaps(DefaultFakeIPRange6) {
			t.Errorf("LAN range %s overlaps a fake-IP range", r)
		}
	}
}

// Fake addresses must not outlive the tunnel in the caches of apps.
func TestFakeIPShortTTL(t *testing.T) {
	for _, selective := range []bool{false, true} {
		o := baseOptions()
		o.Selective = selective
		o.DNS.ProxySuffixes = []string{"example.com"}
		n := 0
		for _, r := range list(sub(render(t, o), "dns"), "rules") {
			if r := r.(map[string]any); r["server"] == tagDNSFakeIP {
				n++
				if r["rewrite_ttl"] != float64(fakeIPTTL) {
					t.Errorf("selective %v: fake-IP rule without the short TTL: %v", selective, r)
				}
			}
		}
		if want := map[bool]int{false: 2, true: 1}[selective]; n != want {
			t.Errorf("selective %v: %d fake-IP rules, want %d", selective, n, want)
		}
	}
}

func TestBuildLeakProtection(t *testing.T) {
	o := baseOptions()
	o.DNS.BlockBrowserDoH = true
	o.DNS.BlockDoT = true
	cfg := render(t, o)

	dnsRules := list(sub(cfg, "dns"), "rules")
	canary := find(dnsRules, map[string]any{"domain": []any{firefoxCanary}})
	if canary == nil || canary["action"] != "predefined" || canary["rcode"] != "NXDOMAIN" {
		t.Errorf("firefox canary rule = %v", canary)
	}
	routeRules := list(sub(cfg, "route"), "rules")
	if find(routeRules, map[string]any{"port": float64(853), "action": "reject"}) == nil {
		t.Error("DoT reject rule missing")
	}
	// Browsers set to use only their own DoH must keep working, through
	// the tunnel.
	doh := ruleIndex(routeRules, map[string]any{"outbound": "proxy", "domain_suffix": toAny(browserDoHDomains)})
	if doh < 0 || find(routeRules, map[string]any{"action": "reject", "domain_suffix": toAny(browserDoHDomains)}) != nil {
		t.Errorf("browser DoH must go through the proxy, not be refused: %v", routeRules)
	}
	if find(dnsRules, map[string]any{"domain_suffix": toAny(browserDoHDomains)}) != nil {
		t.Error("browser DoH resolvers must resolve")
	}
	// Even when the Russian preset or the selective mode sends others direct.
	o.Selective = true
	o.DNS.ProxySuffixes = []string{"youtube.com"}
	if ruleIndex(list(sub(render(t, o), "route"), "rules"), map[string]any{"outbound": "proxy", "domain_suffix": toAny(browserDoHDomains)}) < 0 {
		t.Error("in the selective mode browser DoH must still go through the tunnel")
	}
}

func TestBuildBypassesCoreTraffic(t *testing.T) {
	o := baseOptions()
	o.BypassProcesses = []string{`C:\Program Files\CoreShift\cores\xray.exe`}
	o.BypassAddresses = []netip.Prefix{netip.MustParsePrefix("203.0.113.10/32")}
	cfg := render(t, o)

	first := list(sub(cfg, "dns"), "rules")[0].(map[string]any)
	if first["server"] != "direct" || first["process_path"] == nil {
		t.Errorf("first DNS rule must send core lookups direct, got %v", first)
	}
	if find(list(sub(cfg, "route"), "rules"), map[string]any{"outbound": "direct", "process_path": toAny(o.BypassProcesses)}) == nil {
		t.Error("route bypass rule missing")
	}
	tun := list(cfg, "inbounds")[0].(map[string]any)
	if !reflect.DeepEqual(tun["route_exclude_address"], []any{"203.0.113.10/32"}) {
		t.Errorf("route_exclude_address = %v", tun["route_exclude_address"])
	}
}

func TestBuildRemoteHostnameBootstrapsViaDirect(t *testing.T) {
	o := baseOptions()
	o.DNS.Remote = "tls://dns.example:8853"
	remote := find(list(sub(render(t, o), "dns"), "servers"), map[string]any{"tag": "remote"})
	if remote["domain_resolver"] != "direct" || remote["server_port"] != float64(8853) || remote["type"] != "tls" {
		t.Errorf("remote server = %v", remote)
	}
}

func TestBuildIPv6(t *testing.T) {
	o := baseOptions()
	o.Address6 = netip.MustParsePrefix("fdfe:dcba:9876::1/126")
	cfg := render(t, o)
	dns := sub(cfg, "dns")
	if _, ok := dns["strategy"]; ok {
		t.Error("strategy must not be ipv4_only when IPv6 is enabled")
	}
	if find(list(dns, "servers"), map[string]any{"inet6_range": "fc00::/18"}) == nil {
		t.Error("fakeip inet6_range missing")
	}
	tun := list(cfg, "inbounds")[0].(map[string]any)
	if len(tun["address"].([]any)) != 2 {
		t.Errorf("tun address = %v", tun["address"])
	}
}

// Sites blocked in Russia on .ru must not go direct with the Russia preset.
func TestProxyRuleSetsWinOverDirect(t *testing.T) {
	o := baseOptions()
	o.DNS.DirectSuffixes = []string{"ru"}
	o.DNS.DirectRuleSets = []RuleSet{{Tag: "geosite-ru", Path: "ru.srs"}}
	o.DNS.ProxyRuleSets = []RuleSet{{Tag: "media-blocked", Path: "blocked.srs"}}
	cfg := render(t, o)
	route := sub(cfg, "route")
	if find(list(route, "rule_set"), map[string]any{"tag": "media-blocked", "type": "local"}) == nil {
		t.Error("proxy rule set not defined")
	}
	rules := list(route, "rules")
	proxy := ruleIndex(rules, map[string]any{"rule_set": []any{"media-blocked"}, "outbound": "proxy"})
	direct := ruleIndex(rules, map[string]any{"domain_suffix": []any{"ru"}, "outbound": "direct"})
	if proxy < 0 || proxy > direct {
		t.Errorf("proxy rule set at %d, direct suffixes at %d", proxy, direct)
	}
	dns := list(sub(cfg, "dns"), "rules")
	fake := ruleIndex(dns, map[string]any{"rule_set": []any{"media-blocked"}, "server": "fakeip"})
	directDNS := ruleIndex(dns, map[string]any{"domain_suffix": []any{"ru"}, "server": "direct"})
	if fake < 0 || fake > directDNS {
		t.Errorf("proxy rule set DNS rule at %d, direct suffixes at %d", fake, directDNS)
	}
}

func TestBuildRuleSets(t *testing.T) {
	o := baseOptions()
	o.DNS.DirectSuffixes = []string{"ru", "lan"}
	o.DNS.DirectRuleSets = []RuleSet{{Tag: "geosite-ru", URL: "https://example.org/geosite-ru.srs"}}
	cfg := render(t, o)
	route := sub(cfg, "route")
	if find(list(route, "rule_set"), map[string]any{"tag": "geosite-ru", "type": "remote", "format": "binary"}) == nil {
		t.Error("rule_set definition missing")
	}
	if find(list(route, "rules"), map[string]any{"rule_set": []any{"geosite-ru"}, "outbound": "direct"}) == nil {
		t.Error("rule_set route rule missing")
	}
	if find(list(sub(cfg, "dns"), "rules"), map[string]any{"domain_suffix": []any{"ru", "lan"}, "server": "direct"}) == nil {
		t.Error("direct suffix DNS rule missing")
	}
}

func TestBuildIPRuleSets(t *testing.T) {
	o := baseOptions()
	o.DNS.DirectRuleSets = []RuleSet{{Tag: "geosite-ru", Path: "/rules/geosite-ru.srs"}}
	o.DNS.DirectIPRuleSets = []RuleSet{{Tag: "geoip-ru", Path: "/rules/geoip-ru.srs"}}
	route := sub(render(t, o), "route")
	if find(list(route, "rule_set"), map[string]any{"tag": "geoip-ru", "type": "local", "path": "/rules/geoip-ru.srs"}) == nil {
		t.Error("local rule set definition missing")
	}
	rules := list(route, "rules")
	idx := func(want map[string]any) int {
		for i, r := range rules {
			if find([]any{r}, want) != nil {
				return i
			}
		}
		return -1
	}
	domain := idx(map[string]any{"rule_set": []any{"geosite-ru"}, "outbound": "direct"})
	resolve := idx(map[string]any{"action": "resolve", "server": "remote", "strategy": "ipv4_only"})
	ip := idx(map[string]any{"rule_set": []any{"geoip-ru"}, "outbound": "direct"})
	// Domain rules first, so names already known to be direct are never
	// looked up through the proxy; then resolve, then the IP rules.
	if domain < 0 || resolve < 0 || ip < 0 || !(domain < resolve && resolve < ip) {
		t.Errorf("rule order: domain %d, resolve %d, ip %d in %v", domain, resolve, ip, rules)
	}
	if find(list(sub(render(t, o), "dns"), "rules"), map[string]any{"rule_set": []any{"geoip-ru"}}) != nil {
		t.Error("an IP rule set cannot select a DNS server")
	}
}

func TestBuildValidation(t *testing.T) {
	cases := map[string]func(*Options){
		"no upstream":     func(o *Options) { o.Upstream = netip.AddrPort{} },
		"no remote":       func(o *Options) { o.DNS.Remote = "" },
		"no direct":       func(o *Options) { o.DNS.Direct = "" },
		"direct hostname": func(o *Options) { o.DNS.Direct = "dns.example" },
		"bad scheme":      func(o *Options) { o.DNS.Remote = "ftp://1.1.1.1" },
		"bad stack":       func(o *Options) { o.Stack = "lwip" },
		"v6 as v4":        func(o *Options) { o.Address = netip.MustParsePrefix("fd00::1/126") },
		"rule set no url": func(o *Options) { o.DNS.DirectRuleSets = []RuleSet{{Tag: "x"}} },
		"rule set both":   func(o *Options) { o.DNS.DirectIPRuleSets = []RuleSet{{Tag: "x", Path: "a", URL: "b"}} },
	}
	for name, mutate := range cases {
		o := baseOptions()
		mutate(&o)
		if _, err := Build(o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseUpstream(t *testing.T) {
	cases := map[string]upstream{
		"1.1.1.1":                          {Type: "udp", Host: "1.1.1.1"},
		"1.1.1.1:5353":                     {Type: "udp", Host: "1.1.1.1", Port: 5353},
		"2606:4700::1111":                  {Type: "udp", Host: "2606:4700::1111"},
		"[2606:4700::1111]:53":             {Type: "udp", Host: "2606:4700::1111", Port: 53},
		"udp://8.8.8.8":                    {Type: "udp", Host: "8.8.8.8"},
		"tcp://8.8.8.8:53":                 {Type: "tcp", Host: "8.8.8.8", Port: 53},
		"tls://1.1.1.1":                    {Type: "tls", Host: "1.1.1.1"},
		"https://1.1.1.1/dns-query":        {Type: "https", Host: "1.1.1.1"},
		"https://dns.example/custom-path":  {Type: "https", Host: "dns.example", Path: "/custom-path"},
		"h3://[2606:4700::1111]/dns-query": {Type: "h3", Host: "2606:4700::1111"},
		"quic://dns.adguard-dns.com":       {Type: "quic", Host: "dns.adguard-dns.com"},
	}
	for in, want := range cases {
		got, err := parseUpstream(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", in, got, want)
		}
	}
	for _, bad := range []string{"", "https://", "ws://1.1.1.1", "tls://1.1.1.1:0"} {
		if _, err := parseUpstream(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestDNSAddress(t *testing.T) {
	if got := DNSAddress(DefaultAddress); got != netip.MustParseAddr("172.19.0.2") {
		t.Errorf("DNSAddress = %s", got)
	}
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func TestIPv6ProbesAnsweredEmptyWithoutIPv6(t *testing.T) {
	probe := map[string]any{"domain": []any{"ipv6.msftconnecttest.com", "ipv6.msftncsi.com"}, "action": "predefined", "rcode": "NOERROR"}
	rules := list(sub(render(t, baseOptions()), "dns"), "rules")
	pi, fi := -1, -1
	for i, r := range rules {
		if find([]any{r}, probe) != nil {
			pi = i
		}
		if find([]any{r}, map[string]any{"server": "fakeip"}) != nil {
			fi = i
		}
	}
	if pi < 0 || fi < 0 || pi > fi {
		t.Errorf("probe rule %d must precede the fake-IP rule %d: %v", pi, fi, rules)
	}

	o := baseOptions()
	o.Address6 = netip.MustParsePrefix("fdfe:dcba:9876::1/126")
	if find(list(sub(render(t, o), "dns"), "rules"), probe) != nil {
		t.Error("with IPv6 the probes must be answered for real")
	}
}

func TestIPv6Tunnel(t *testing.T) {
	o := baseOptions()
	o.Address6 = DefaultAddress6
	o.DNS.DirectSuffixes = []string{"ru"}
	o.DNS.DirectIPv4Only = true
	o.DNS.DirectIPRuleSets = []RuleSet{{Tag: "geoip-ru", Path: "/rules/geoip-ru.srs"}}
	cfg := render(t, o)
	dns := sub(cfg, "dns")
	if find(list(dns, "rules"), map[string]any{"domain_suffix": []any{"ru"}, "query_type": []any{"AAAA"}, "action": "predefined"}) == nil {
		t.Error("direct names must be IPv4-only on a host without IPv6")
	}
	if find(list(dns, "servers"), map[string]any{"type": "fakeip", "inet6_range": DefaultFakeIPRange6.String()}) == nil {
		t.Error("no IPv6 fake-IP range")
	}
	if find(list(sub(cfg, "route"), "rules"), map[string]any{"action": "resolve", "strategy": "prefer_ipv4"}) == nil {
		t.Error("resolve must allow IPv6 when the tunnel has it")
	}
	v6ToProxy := map[string]any{"ip_cidr": []any{"2000::/3"}, "outbound": tagProxy}
	routeRules := list(sub(cfg, "route"), "rules")
	vi, di := -1, -1
	for i, r := range routeRules {
		if find([]any{r}, v6ToProxy) != nil {
			vi = i
		}
		if find([]any{r}, map[string]any{"domain_suffix": []any{"ru"}, "outbound": tagDirect}) != nil {
			di = i
		}
	}
	if vi < 0 || di < 0 || vi > di {
		t.Errorf("real IPv6 addresses (rule %d) must go to the proxy before direct rules (rule %d)", vi, di)
	}

	o.DNS.DirectIPv4Only = false
	cfg = render(t, o)
	if find(list(sub(cfg, "dns"), "rules"), map[string]any{"domain_suffix": []any{"ru"}, "query_type": []any{"AAAA"}}) != nil {
		t.Error("a host with IPv6 must get IPv6 addresses for direct names")
	}
	if find(list(sub(cfg, "route"), "rules"), v6ToProxy) != nil {
		t.Error("a host with IPv6 must reach direct sites over its own IPv6")
	}
}

func TestDirectApps(t *testing.T) {
	o := baseOptions()
	o.DirectApps = []string{"qbittorrent.exe", "Steam.exe"}
	rules := list(sub(render(t, o), "route"), "rules")
	var rule map[string]any
	for _, r := range rules {
		if m := r.(map[string]any); m["process_path_regex"] != nil {
			rule = m
		}
	}
	if rule == nil || rule["outbound"] != "direct" {
		t.Fatalf("no direct rule for the apps in %v", rules)
	}
	pats := rule["process_path_regex"].([]any)
	re := regexp.MustCompile(pats[1].(string))
	for path, want := range map[string]bool{
		`C:\Program Files (x86)\Steam\steam.exe`: true,
		`D:\Games\STEAM.EXE`:                     true,
		`/usr/bin/Steam.exe`:                     true,
		`Steam.exe`:                              true,
		`C:\Tools\notSteam.exe`:                  false,
		`C:\Steam.exe\other.exe`:                 false,
	} {
		if re.MatchString(path) != want {
			t.Errorf("%s matched %v, want %v", path, !want, want)
		}
	}
	if !regexp.MustCompile(pats[0].(string)).MatchString(`C:\Program Files\qBittorrent\qbittorrent.exe`) {
		t.Error("qbittorrent.exe not matched")
	}
}

// ruleIndex is the position of the first rule with all of match, or -1.
func ruleIndex(rules []any, match map[string]any) int {
	for i, r := range rules {
		if find([]any{r}, match) != nil {
			return i
		}
	}
	return -1
}

func TestProxyListsWinOverDirect(t *testing.T) {
	o := baseOptions()
	o.DNS.DirectSuffixes = []string{"ru"}
	o.DNS.ProxySuffixes = []string{"blocked.ru"}
	o.DNS.BlockSuffixes = []string{"ads.example"}
	o.DirectIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	o.ProxyIPs = []netip.Prefix{netip.MustParsePrefix("91.108.4.0/22")}
	o.ProxyApps = []string{"Telegram.exe"}
	cfg := render(t, o)
	rules := list(sub(cfg, "route"), "rules")

	proxyDomain := ruleIndex(rules, map[string]any{"domain_suffix": []any{"blocked.ru"}, "outbound": "proxy"})
	directDomain := ruleIndex(rules, map[string]any{"domain_suffix": []any{"ru"}, "outbound": "direct"})
	if proxyDomain < 0 || directDomain < 0 || proxyDomain > directDomain {
		t.Errorf("proxy domains must come before direct ones: %d, %d in %v", proxyDomain, directDomain, rules)
	}
	if ruleIndex(rules, map[string]any{"ip_cidr": []any{"91.108.4.0/22"}, "outbound": "proxy"}) < 0 ||
		ruleIndex(rules, map[string]any{"ip_cidr": []any{"203.0.113.0/24"}, "outbound": "direct"}) < 0 {
		t.Errorf("no IP rules in %v", rules)
	}
	if ruleIndex(rules, map[string]any{"domain_suffix": []any{"ads.example"}, "action": "reject"}) < 0 {
		t.Errorf("blocked domains are not rejected: %v", rules)
	}
	apps := ruleIndex(rules, map[string]any{"outbound": "proxy", "process_path_regex": []any{`(?i)(^|[\\/])Telegram\.exe$`}})
	if apps < 0 || apps > directDomain {
		t.Errorf("proxy apps rule at %d in %v", apps, rules)
	}
	if sub(cfg, "route")["final"] != "proxy" {
		t.Error("everything else must go through the proxy")
	}

	dns := list(sub(cfg, "dns"), "rules")
	if find(dns, map[string]any{"domain_suffix": []any{"ads.example"}, "rcode": "NXDOMAIN"}) == nil {
		t.Errorf("blocked domains resolve: %v", dns)
	}
	proxyDNS := ruleIndex(dns, map[string]any{"domain_suffix": []any{"blocked.ru"}, "server": "fakeip"})
	directDNS := ruleIndex(dns, map[string]any{"domain_suffix": []any{"ru"}, "server": "direct"})
	if proxyDNS < 0 || directDNS < 0 || proxyDNS > directDNS {
		t.Errorf("proxy domains must be resolved through the tunnel first: %v", dns)
	}
}

func TestSelective(t *testing.T) {
	o := baseOptions()
	o.Selective = true
	o.Address6 = DefaultAddress6
	o.DNS.DirectIPv4Only = true
	o.DNS.ProxySuffixes = []string{"youtube.com"}
	o.DNS.DirectSuffixes = []string{"lan"}
	o.DNS.DirectRuleSets = []RuleSet{{Tag: "geosite-ru", URL: "https://example.org/geosite-ru.srs"}}
	o.DirectIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	o.ProxyIPs = []netip.Prefix{netip.MustParsePrefix("91.108.4.0/22")}
	o.BypassProcesses = []string{`C:\CoreShift\cores\xray.exe`}
	cfg := render(t, o)
	route := sub(cfg, "route")
	if route["final"] != "direct" {
		t.Errorf("route final = %v", route["final"])
	}
	if route["rule_set"] != nil {
		t.Error("selective mode needs no rule sets")
	}
	rules := list(route, "rules")
	for _, r := range rules {
		m := r.(map[string]any)
		if m["outbound"] == "proxy" && m["domain_suffix"] == nil && m["ip_cidr"] == nil {
			t.Errorf("unexpected proxy rule %v", m)
		}
		if reflect.DeepEqual(m["ip_cidr"], []any{"2000::/3"}) {
			t.Error("IPv6 of other sites must not be sent through the tunnel")
		}
	}
	if ruleIndex(rules, map[string]any{"domain_suffix": []any{"youtube.com"}, "outbound": "proxy"}) < 0 ||
		ruleIndex(rules, map[string]any{"ip_cidr": []any{"91.108.4.0/22"}, "outbound": "proxy"}) < 0 {
		t.Errorf("selected traffic is not proxied: %v", rules)
	}

	d := sub(cfg, "dns")
	if d["final"] != "direct" {
		t.Errorf("dns final = %v", d["final"])
	}
	dns := list(d, "rules")
	fake := ruleIndex(dns, map[string]any{"domain_suffix": []any{"youtube.com"}, "server": "fakeip"})
	aaaa := ruleIndex(dns, map[string]any{"query_type": []any{"AAAA"}, "rcode": "NOERROR"})
	if fake < 0 || aaaa < 0 || aaaa < fake {
		t.Errorf("selected names need fake addresses before other AAAA are emptied: %v", dns)
	}
	for _, r := range dns {
		if m := r.(map[string]any); m["server"] == "fakeip" && m["domain_suffix"] == nil {
			t.Errorf("other names must get real addresses: %v", m)
		}
	}
}

func TestDirectAppsOnIPv4OnlyHost(t *testing.T) {
	o := baseOptions()
	o.DirectApps = []string{"music.exe"}
	o.Address6 = DefaultAddress6
	o.DNS.DirectIPv4Only = true
	rules := list(sub(render(t, o), "route"), "rules")
	reject := ruleIndex(rules, map[string]any{"ip_cidr": []any{"2000::/3"}, "action": "reject", "no_drop": true})
	direct := ruleIndex(rules, map[string]any{"outbound": "direct", "process_path_regex": []any{`(?i)(^|[\\/])music\.exe$`}})
	if reject < 0 || direct < 0 || reject > direct {
		t.Errorf("IPv6 of direct apps must be refused before they go direct: %d, %d in %v", reject, direct, rules)
	}
	if m := rules[reject].(map[string]any); m["process_path_regex"] == nil {
		t.Errorf("only the direct apps' IPv6 may be refused: %v", m)
	}

	o.DNS.DirectIPv4Only = false
	if ruleIndex(list(sub(render(t, o), "route"), "rules"), map[string]any{"action": "reject", "ip_cidr": []any{"2000::/3"}}) >= 0 {
		t.Error("a host with IPv6 must reach direct IPv6 addresses")
	}
}

func TestPlatformTUN(t *testing.T) {
	o := baseOptions()
	o.Platform = true
	o.BypassProcesses = []string{`C:\cores\xray.exe`}
	o.DirectApps = []string{"telegram.exe"}
	o.BypassAddresses = []netip.Prefix{netip.MustParsePrefix("203.0.113.5/32")}
	cfg := render(t, o)
	if sub(cfg, "route")["auto_detect_interface"] != false {
		t.Error("auto_detect_interface on with a platform TUN")
	}
	if stack := list(cfg, "inbounds")[0].(map[string]any)["stack"]; stack != "gvisor" {
		t.Errorf("stack = %v with a platform TUN, want gvisor", stack)
	}
	b, _ := Build(o)
	for _, bad := range []string{"process_path", "route_exclude_address"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("%s in a platform config", bad)
		}
	}
}

// Linux keeps the local network out of the TUN routes, so replies to
// connections made to the machine leave by its own interface.
func TestExcludeLAN(t *testing.T) {
	server := netip.MustParsePrefix("203.0.113.7/32")
	base := Options{Upstream: netip.MustParseAddrPort("127.0.0.1:17890"), DNS: DNSOptions{Remote: "1.1.1.1", Direct: "192.168.1.1"}, BypassAddresses: []netip.Prefix{server}}
	excluded := func(o Options) []any {
		cfg, err := build(o)
		if err != nil {
			t.Fatal(err)
		}
		list, _ := cfg["inbounds"].([]any)[0].(obj)["route_exclude_address"].([]string)
		out := make([]any, len(list))
		for i, v := range list {
			out[i] = v
		}
		return out
	}
	if got := excluded(base); len(got) != 1 || got[0] != server.String() {
		t.Fatalf("without ExcludeLAN: %v", got)
	}
	lan := base
	lan.ExcludeLAN = true
	got := excluded(lan)
	if len(got) != 1+len(lanRanges) || got[0] != server.String() {
		t.Fatalf("with ExcludeLAN: %v", got)
	}
	for _, want := range []string{"192.168.0.0/16", "10.0.0.0/8", "172.16.0.0/12", "fe80::/10"} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("%s not excluded: %v", want, got)
		}
	}
	// Android's VpnService owns the routes: nothing is excluded there.
	lan.Platform = true
	if got := excluded(lan); len(got) != 0 {
		t.Errorf("platform TUN: %v", got)
	}
}
