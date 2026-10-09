// Package tunlayer builds the configuration of the persistent TUN + DNS layer.
//
// The layer is a sing-box instance that owns the TUN interface, answers and
// hijacks all DNS, and forwards traffic to the SOCKS inbound of whichever core
// (xray, sing-box, mihomo) is currently active. Swapping cores therefore never
// tears down the interface, routes or DNS settings, so nothing leaks mid-swap.
//
// Target schema: sing-box 1.12+; validated against 1.14.2 by
// TestSingBoxAcceptsConfig.
package tunlayer

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"regexp"
	"slices"
)

const (
	DefaultInterface = "coreshift"
	DefaultMTU       = 9000
	DefaultStack     = "mixed"

	tagTun       = "tun-in"
	tagProxy     = "proxy"
	tagDirect    = "direct"
	tagDNSRemote = "remote"
	tagDNSDirect = "direct"
	tagDNSFakeIP = "fakeip"
)

// fakeIPTTL is the TTL of fake addresses, in seconds. The tunnel answers
// every lookup itself, so repeating them is cheap.
const fakeIPTTL = 1

// dnsTimeout bounds every lookup (sing-box's default is 10 s). The remote
// server's connection runs through the proxy and can die without a word:
// the server or a carrier NAT drops it while a phone sleeps, and nothing on
// this side notices. sing-box replaces it only once a query on it times out
// (dns/transport/https.go, and the TCP/TLS/UDP multiplexer), so until then
// every lookup hangs on it. Timing out sooner replaces it sooner, before
// the system resolvers' own retry (Android's and glibc's come after 5 s),
// which then goes out on a fresh connection. A new connection through the
// proxy needs a few round trips to the server, well inside this.
const dnsTimeout = "4s"

var (
	DefaultAddress      = netip.MustParsePrefix("172.19.0.1/30")
	DefaultAddress6     = netip.MustParsePrefix("fdfe:dcba:9876::1/126")
	DefaultFakeIPRange  = netip.MustParsePrefix("198.18.0.0/15")
	DefaultFakeIPRange6 = netip.MustParsePrefix("fc00::/18")
)

// lanRanges always go direct. Listed explicitly instead of ip_is_private so
// the fake-IP ranges can never be caught by it.
var lanRanges = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "224.0.0.0/4",
	"fd00::/8", "fe80::/10", "ff00::/8",
}

// browserDoHDomains are resolvers browsers use for built-in DNS over HTTPS,
// which bypasses our DNS (and fake-IP). With BlockBrowserDoH they are only
// reached through the proxy.
var browserDoHDomains = []string{
	"dns.google", "dns.google.com", "cloudflare-dns.com", "one.one.one.one",
	"dns.quad9.net", "doh.opendns.com", "dns.nextdns.io", "doh.cleanbrowsing.org",
	"dns.adguard-dns.com", "doh.dns.sb", "dns.alidns.com", "doh.pub",
}

// ipv6Probes are the hosts Windows checks IPv6 connectivity with. They have
// only IPv6 addresses, so over an IPv4-only tunnel every check would fail
// with an error in the log; answering them empty gives Windows the same
// verdict, no IPv6, quietly.
var ipv6Probes = []string{"ipv6.msftconnecttest.com", "ipv6.msftncsi.com"}

// firefoxCanary: answering NXDOMAIN tells Firefox not to turn on DoH by default.
const firefoxCanary = "use-application-dns.net"

// Options configures the layer. Zero values fall back to the defaults above.
type Options struct {
	InterfaceName string
	Address       netip.Prefix // IPv4 TUN address
	Address6      netip.Prefix // optional; zero means IPv4-only
	// RefuseIPv6 is IPv6 switched off by the user: the tunnel is IPv4-only,
	// yet IPv6 must not leave by the host's own network either, where it
	// would show the real address (sites reached through a browser's own
	// DoH, WebRTC, addresses apps hold). The TUN still takes all of it,
	// with Address6 (DefaultAddress6 if unset), and refuses it before a
	// connection is made, so apps fall back to IPv4 at once; DNS sent over
	// IPv6 is still answered. The local network stays out (ExcludeLAN).
	// Needs a system that gives the interface IPv6; where it does not,
	// leave it unset: strict_route then makes IPv6 unreachable (sing-tun:
	// an unreachable rule on Linux, a WFP block on Windows). Ignored with
	// Platform, whose TUN blocks a family it has no address of (Android's
	// VpnService).
	RefuseIPv6 bool
	MTU        uint32
	Stack      string // "mixed", "system" or "gvisor"
	// StrictRoute makes sing-box block traffic that tries to bypass the TUN;
	// on Windows this is what stops DNS leaking to physical adapters (WFP).
	StrictRoute bool

	// Upstream is the SOCKS5 inbound of the active core, and UpstreamUser
	// and UpstreamPass the credentials it requires, if any.
	Upstream     netip.AddrPort
	UpstreamUser string
	UpstreamPass string
	// BypassProcesses are core executables (full paths); their own traffic
	// must go direct or it would loop back into the tunnel.
	BypassProcesses []string
	// DirectDNSProcesses (full paths) have their DNS queries answered by the
	// direct resolver with real addresses, while their traffic still goes
	// through the tunnel: the daemon itself, which looks up proxy servers.
	DirectDNSProcesses []string
	// BypassAddresses are proxy server IPs, excluded from TUN routes for the
	// same reason. Needed where process matching is unavailable.
	BypassAddresses []netip.Prefix
	// ExcludeLAN keeps the local network ranges out of the TUN routes, so
	// they take the system's own routes: on Windows a Hyper-V or WSL
	// switch, Docker or a second adapter is reached by its own interface,
	// where the layer's direct outbound would leave by the default one; on
	// Linux the replies to connections made to this machine from the local
	// network (SSH, file sharing, a VM's host) no longer die in the TUN.
	// Those ranges go direct anyway. LANResolvers are carved out of them.
	// On Linux only DNS to them goes into the TUN (service resolver rules).
	// On Android the VpnService gets the same list (see mobile).
	ExcludeLAN bool
	// LANResolvers are the system's resolvers (a home router, WSL's host):
	// with ExcludeLAN they stay routed into the TUN, so DNS sent to them
	// straight, past the DNS guard, is still hijacked instead of leaking.
	// Addresses outside the private ranges, loopback and link-local ones
	// (which need a zone) are ignored.
	LANResolvers []netip.Addr
	// DirectApps are executable names ("qbittorrent.exe") whose traffic goes
	// direct instead of through the proxy, wherever they are installed.
	// Names match case-insensitively.
	DirectApps []string
	// AppFilter and FilterApps are Android's per-app VPN (store.Routing):
	// the VpnService takes them, sing-box never sees them.
	AppFilter  string
	FilterApps []string
	// DirectIPs are addresses and subnets that go direct. They match
	// connections made by address; names are matched by DNS.DirectSuffixes.
	DirectIPs []netip.Prefix

	// ProxyApps, ProxyIPs and DNS.ProxySuffixes always go through the proxy,
	// winning over every direct list except DirectApps.
	ProxyApps []string
	ProxyIPs  []netip.Prefix
	// Selective sends only the proxy lists through the proxy and everything
	// else direct; the direct lists and rule sets then have nothing to do.
	Selective bool

	// Rules are the user's own rules, in both modes, in order: after the
	// user's lists and before the presets (see DNSOptions.DirectSuffixes).
	// Those of address sets (geoip) come after every rule by name, just
	// before DNS.DirectIPRuleSets: an address set needs the address of a
	// name, looked up through the tunnel, as v2ray's IPIfNonMatch does,
	// and a name decided before is not looked up so.
	Rules []Rule

	DNS DNSOptions

	// CacheFile persists fake-IP mappings across restarts; empty disables it.
	CacheFile string

	// Platform: the TUN comes from the platform (Android's VpnService), which
	// keeps the app itself, and so the cores it starts, outside the VPN.
	// Outbound sockets then need no binding to the physical interface, and
	// matching processes is left to the platform: the process lists are
	// ignored, and the stack is gVisor.
	Platform bool

	LogLevel string
}

type DNSOptions struct {
	// Remote resolves everything not routed direct, through the proxy.
	Remote string
	// Direct resolves direct domains and the cores' own lookups, bypassing
	// the proxy. Usually a resolver captured from the system before connecting.
	// Must be an IP address.
	Direct string

	FakeIP       bool
	FakeIPRange  netip.Prefix
	FakeIPRange6 netip.Prefix

	// The rules go in this order, the first that matches deciding: the
	// user's lists (BlockSuffixes, the apps, ProxySuffixes and ProxyIPs,
	// DirectIPs and DirectSuffixes), the user's Rules, then the presets
	// (BlockRuleSets, PinnedSuffixes and PinnedRuleSets, ProxyRuleSets,
	// HomeSuffixes and DirectRuleSets, DirectIPRuleSets), then the default:
	// the proxy, or direct with Selective.
	//
	// DirectSuffixes are the user's: they go direct wherever the site is.
	DirectSuffixes []string
	// BlockRuleSets are domain sets refused (ads), unless the user's lists
	// or Rules let a name through.
	BlockRuleSets []RuleSet
	// PinnedSuffixes and PinnedRuleSets (domain sets) always go through the
	// proxy, unless the user's lists or Rules say otherwise: Google with
	// the Russian preset.
	PinnedSuffixes []string
	PinnedRuleSets []RuleSet
	// HomeSuffixes are a preset's direct names, like DirectRuleSets
	// (geosite) after its proxy sets; both are resolved directly. With
	// HomeCheck they go direct only where the address is in
	// DirectIPRuleSets, and through the proxy otherwise: sites of the
	// country the user is in go direct where they are hosted there, and
	// through the tunnel where they are abroad (behind a foreign CDN or
	// hosting, which the provider may slow down).
	HomeSuffixes []string
	HomeCheck    bool
	// ProxySuffixes are the user's names that go through the proxy, ahead
	// of every list but the block list.
	ProxySuffixes []string
	// BlockSuffixes are answered NXDOMAIN and their connections refused.
	BlockSuffixes []string
	// DirectRuleSets are domain rule sets (geosite) resolved and routed direct.
	DirectRuleSets []RuleSet
	// ProxyRuleSets are domain rule sets resolved through the tunnel and
	// routed through the proxy even when a direct suffix or rule set also
	// matches them: sites blocked where the user is, on domains that go
	// direct otherwise (novayagazeta.ru under "ru").
	ProxyRuleSets []RuleSet
	// DirectIPRuleSets are IP rule sets (geoip) routed direct. Matching them
	// needs the real address, so connections by name are resolved through
	// the remote DNS first; the proxy then gets the address instead of the
	// name.
	DirectIPRuleSets []RuleSet

	// DirectIPv4Only answers direct names with IPv4 addresses only, for hosts
	// without IPv6 of their own: direct traffic cannot use the tunnel's IPv6,
	// and apps would first try addresses they cannot reach. Only matters
	// with Address6; without it everything is IPv4-only anyway.
	DirectIPv4Only bool

	// BlockBrowserDoH keeps browsers' own DNS over HTTPS inside the tunnel:
	// Firefox is told not to turn it on by itself, so it uses the system
	// DNS, which we hijack, and browsers that have it on anyway reach their
	// resolvers only through the proxy. (Refusing the resolvers instead
	// broke browsers set to use nothing else, like Firefox's maximum
	// protection.) The name is kept from then, as is the setting's.
	BlockBrowserDoH bool
	// BlockDoT rejects port 853, e.g. Android Private DNS in automatic mode.
	BlockDoT bool
}

// RuleSet is a binary (.srs) rule set, e.g. geosite-category-ru: a local
// file (Path) or one sing-box downloads itself (URL).
type RuleSet struct {
	Tag  string
	Path string
	URL  string
}

// What a Rule does with what it matches.
const (
	ActionProxy  = "proxy"
	ActionDirect = "direct"
	ActionBlock  = "block"
)

// Rule is one of the user's own rules: what it matches, one of Domains
// (suffixes), IPs or Set, and Action. See Options.Rules for the order.
type Rule struct {
	Action  string
	Domains []string
	IPs     []netip.Prefix
	// Set is a rule set of names (geosite), or of addresses (geoip) with
	// SetIP.
	Set   *RuleSet
	SetIP bool
}

func (r Rule) validate() error {
	n := 0
	for _, has := range []bool{len(r.Domains) > 0, len(r.IPs) > 0, r.Set != nil} {
		if has {
			n++
		}
	}
	if n != 1 {
		return errors.New("tunlayer: a rule needs exactly one of domains, addresses or a rule set")
	}
	if !slices.Contains([]string{ActionProxy, ActionDirect, ActionBlock}, r.Action) {
		return fmt.Errorf("tunlayer: unknown rule action %q", r.Action)
	}
	if r.SetIP && r.Set == nil {
		return errors.New("tunlayer: an address rule set rule without a rule set")
	}
	return nil
}

// byName reports whether r is decided by the name or the address a
// connection has: every rule but one of an address set, which needs the
// address of a name looked up first.
func (r Rule) byName() bool { return !r.SetIP }

// match returns the condition of r for route rules, and with dns for DNS
// rules (nil for rules that DNS cannot decide: addresses).
func (r Rule) match(dns bool) obj {
	switch {
	case len(r.Domains) > 0:
		return obj{"domain_suffix": r.Domains}
	case r.Set != nil && (!dns || !r.SetIP):
		return obj{"rule_set": []string{r.Set.Tag}}
	case len(r.IPs) > 0 && !dns:
		return obj{"ip_cidr": prefixStrings(r.IPs)}
	}
	return nil
}

// routeRule returns the route rule of r.
func (r Rule) routeRule() obj {
	m := r.match(false)
	switch r.Action {
	case ActionBlock:
		m["action"] = "reject"
	case ActionDirect:
		m["outbound"] = tagDirect
	default:
		m["outbound"] = tagProxy
	}
	return m
}

// DNSAddress is the resolver address sing-box assigns to the TUN interface:
// the one right after the interface's own address. Point dnsguard at it.
func DNSAddress(p netip.Prefix) netip.Addr { return p.Addr().Next() }

type obj = map[string]any

// Build renders the sing-box configuration as JSON.
func Build(o Options) ([]byte, error) {
	cfg, err := build(o)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func (o Options) withDefaults() Options {
	if o.Platform {
		o.BypassProcesses, o.DirectDNSProcesses, o.DirectApps, o.ProxyApps = nil, nil, nil, nil
		o.BypassAddresses = nil
		o.ExcludeLAN, o.LANResolvers = false, nil
		o.RefuseIPv6 = false
		// The system stack answers TCP from a kernel socket of this process,
		// which Android keeps outside its own VPN: the answers leave by the
		// physical network and every TCP connection hangs. gVisor answers
		// through the TUN itself.
		o.Stack = "gvisor"
	}
	if o.InterfaceName == "" {
		o.InterfaceName = DefaultInterface
	}
	if o.RefuseIPv6 {
		if !o.Address6.IsValid() {
			o.Address6 = DefaultAddress6
		}
		o.DNS.DirectIPv4Only = false // nothing IPv6 goes direct anyway
	}
	if !o.Address.IsValid() {
		o.Address = DefaultAddress
	}
	if o.MTU == 0 {
		o.MTU = DefaultMTU
	}
	if o.Stack == "" {
		o.Stack = DefaultStack
	}
	if o.LogLevel == "" {
		o.LogLevel = "warn"
	}
	if o.DNS.FakeIP && !o.DNS.FakeIPRange.IsValid() {
		o.DNS.FakeIPRange = DefaultFakeIPRange
	}
	if o.DNS.FakeIP && o.ipv6() && !o.DNS.FakeIPRange6.IsValid() {
		o.DNS.FakeIPRange6 = DefaultFakeIPRange6
	}
	if !o.ipv6() {
		o.DNS.FakeIPRange6 = netip.Prefix{}
	}
	return o
}

// ipv6 reports whether IPv6 goes through the tunnel: it has an IPv6
// address, and IPv6 is not refused there.
func (o Options) ipv6() bool { return o.Address6.IsValid() && !o.RefuseIPv6 }

func (o Options) validate() error {
	if !o.Upstream.IsValid() || o.Upstream.Port() == 0 {
		return errors.New("tunlayer: upstream SOCKS address is required")
	}
	if !o.Address.Addr().Is4() {
		return errors.New("tunlayer: Address must be an IPv4 prefix")
	}
	if o.Address6.IsValid() && !o.Address6.Addr().Is6() {
		return errors.New("tunlayer: Address6 must be an IPv6 prefix")
	}
	if !slices.Contains([]string{"mixed", "system", "gvisor"}, o.Stack) {
		return fmt.Errorf("tunlayer: unknown stack %q", o.Stack)
	}
	if o.DNS.Remote == "" {
		return errors.New("tunlayer: remote DNS server is required")
	}
	if o.DNS.Direct == "" {
		return errors.New("tunlayer: direct DNS server is required")
	}
	for _, r := range o.Rules {
		if err := r.validate(); err != nil {
			return err
		}
	}
	seen := map[string]RuleSet{}
	for _, rs := range o.ruleSets(true) {
		if rs.Tag == "" || (rs.Path == "") == (rs.URL == "") {
			return errors.New("tunlayer: rule set needs a tag and either a path or a URL")
		}
		if prev, ok := seen[rs.Tag]; ok && prev != rs {
			return fmt.Errorf("tunlayer: two rule sets tagged %q", rs.Tag)
		}
		seen[rs.Tag] = rs
	}
	return nil
}

// ruleSets returns the rule sets the configuration refers to, or with all
// every one it was given; Selective mode never refers to the direct ones.
// One used twice (by a preset and by a rule) is in the list twice.
func (o Options) ruleSets(all bool) []RuleSet {
	sets := slices.Concat(o.DNS.BlockRuleSets, o.DNS.PinnedRuleSets, o.DNS.ProxyRuleSets)
	if all || !o.Selective {
		sets = slices.Concat(sets, o.DNS.DirectRuleSets, o.DNS.DirectIPRuleSets)
	}
	for _, r := range o.Rules {
		if r.Set != nil {
			sets = append(sets, *r.Set)
		}
	}
	return sets
}

func build(o Options) (obj, error) {
	o = o.withDefaults()
	if err := o.validate(); err != nil {
		return nil, err
	}
	dns, err := buildDNS(o)
	if err != nil {
		return nil, err
	}

	addrs := []string{o.Address.String()}
	if o.Address6.IsValid() {
		addrs = append(addrs, o.Address6.String())
	}
	tun := obj{
		"type":           "tun",
		"tag":            tagTun,
		"interface_name": o.InterfaceName,
		"address":        addrs,
		"mtu":            o.MTU,
		"auto_route":     true,
		"strict_route":   o.StrictRoute,
		"stack":          o.Stack,
		// Linux policy routing: our own table and rule priorities rather
		// than sing-box's defaults, so CleanupRoutes can tell the rules a
		// killed TUN layer left from those of anything else.
		"iproute2_table_index": RouteTable,
		"iproute2_rule_index":  RuleIndex,
	}
	exclude := prefixStrings(o.BypassAddresses)
	if o.ExcludeLAN {
		exclude = append(exclude, prefixStrings(excludeLAN(o.LANResolvers))...)
	}
	if len(exclude) > 0 {
		tun["route_exclude_address"] = exclude
	}

	upstream := obj{
		"type":        "socks",
		"tag":         tagProxy,
		"server":      o.Upstream.Addr().String(),
		"server_port": o.Upstream.Port(),
		"version":     "5",
	}
	if o.UpstreamUser != "" {
		upstream["username"], upstream["password"] = o.UpstreamUser, o.UpstreamPass
	}
	cfg := obj{
		"log":       obj{"level": o.LogLevel, "timestamp": true},
		"dns":       dns,
		"inbounds":  []any{tun},
		"outbounds": []any{upstream, obj{"type": "direct", "tag": tagDirect}},
		"route":     buildRoute(o),
	}
	if o.CacheFile != "" {
		cfg["experimental"] = obj{"cache_file": obj{
			"enabled":      true,
			"path":         o.CacheFile,
			"store_fakeip": o.DNS.FakeIP,
		}}
	}
	return cfg, nil
}

func buildDNS(o Options) (obj, error) {
	remote, err := parseUpstream(o.DNS.Remote)
	if err != nil {
		return nil, err
	}
	directUp, err := parseUpstream(o.DNS.Direct)
	if err != nil {
		return nil, err
	}
	if !directUp.isIP() {
		return nil, fmt.Errorf("tunlayer: direct DNS server %q must be an IP address", o.DNS.Direct)
	}
	if a, _ := netip.ParseAddr(directUp.Host); a.IsLinkLocalUnicast() && a.Zone() == "" {
		// Every lookup would fail on dialling it ("invalid argument").
		return nil, fmt.Errorf("tunlayer: direct DNS server %q is link-local and needs an interface (%%zone)", o.DNS.Direct)
	}

	remoteSrv := remote.server(tagDNSRemote)
	remoteSrv["detour"] = tagProxy
	if !remote.isIP() {
		remoteSrv["domain_resolver"] = tagDNSDirect
	}
	// No detour: new-style DNS servers dial directly by default.
	servers := []any{remoteSrv, directUp.server(tagDNSDirect)}

	var rules []any
	if procs := slices.Concat(o.BypassProcesses, o.DirectDNSProcesses); len(procs) > 0 {
		// Cores resolving their proxy server must not depend on the proxy,
		// and fake addresses are of no use to the daemon.
		rules = append(rules, obj{"process_path": procs, "server": tagDNSDirect})
	}
	if len(o.DNS.BlockSuffixes) > 0 {
		rules = append(rules, obj{"domain_suffix": o.DNS.BlockSuffixes, "action": "predefined", "rcode": "NXDOMAIN"})
	}
	if !o.ipv6() {
		rules = append(rules, obj{"domain": ipv6Probes, "action": "predefined", "rcode": "NOERROR"})
	}
	if o.DNS.BlockBrowserDoH {
		rules = append(rules, obj{"domain": []string{firefoxCanary}, "action": "predefined", "rcode": "NXDOMAIN"})
	}
	if o.DNS.FakeIP {
		fake := obj{"type": "fakeip", "tag": tagDNSFakeIP, "inet4_range": o.DNS.FakeIPRange.String()}
		if o.DNS.FakeIPRange6.IsValid() {
			fake["inet6_range"] = o.DNS.FakeIPRange6.String()
		}
		servers = append(servers, fake)
	}
	// fakeIP answers A and AAAA queries (of names matched by key, if any)
	// with fake addresses. Their TTL is cut from sing-box's 600 s to
	// fakeIPTTL: apps that cache names themselves (browsers, games,
	// Electron apps) would otherwise keep dialling a fake address for up to
	// ten minutes after the tunnel is gone, and nothing but the tunnel can
	// reach one.
	fakeIP := func(key string, value any) {
		rule := obj{"query_type": []string{"A", "AAAA"}, "server": tagDNSFakeIP, "rewrite_ttl": fakeIPTTL}
		if key != "" {
			rule[key] = value
		}
		rules = append(rules, rule)
	}
	// proxied sends names matched by key to the tunnel's resolver, or fake
	// addresses for them.
	proxied := func(key string, value any) {
		if o.DNS.FakeIP {
			fakeIP(key, value)
		}
		rules = append(rules, obj{key: value, "server": tagDNSRemote})
	}
	// direct sends names matched by key to the direct resolver; on a host
	// without IPv6 their AAAA queries are answered empty first. An empty key
	// matches every name.
	direct := func(key string, value any) {
		aaaa := obj{"query_type": []string{"AAAA"}, "action": "predefined", "rcode": "NOERROR"}
		to := obj{"server": tagDNSDirect}
		if key != "" {
			aaaa[key], to[key] = value, value
		}
		if o.DNS.DirectIPv4Only && o.ipv6() {
			rules = append(rules, aaaa)
		}
		if key != "" {
			rules = append(rules, to)
		}
	}
	// In the order of the route rules (buildRoute): the user's lists, the
	// user's rules, the presets.
	if len(o.DNS.ProxySuffixes) > 0 {
		proxied("domain_suffix", o.DNS.ProxySuffixes)
	}
	if !o.Selective && len(o.DNS.DirectSuffixes) > 0 {
		direct("domain_suffix", o.DNS.DirectSuffixes)
	}
	for _, r := range o.Rules {
		for key, value := range r.match(true) { // the one condition, if any
			switch r.Action {
			case ActionBlock:
				rules = append(rules, obj{key: value, "action": "predefined", "rcode": "NXDOMAIN"})
			case ActionDirect:
				direct(key, value)
			default:
				proxied(key, value)
			}
		}
	}
	if len(o.DNS.BlockRuleSets) > 0 {
		rules = append(rules, obj{"rule_set": ruleSetTags(o.DNS.BlockRuleSets), "action": "predefined", "rcode": "NXDOMAIN"})
	}
	if len(o.DNS.PinnedSuffixes) > 0 {
		proxied("domain_suffix", o.DNS.PinnedSuffixes)
	}
	if len(o.DNS.PinnedRuleSets) > 0 {
		proxied("rule_set", ruleSetTags(o.DNS.PinnedRuleSets))
	}
	if len(o.DNS.ProxyRuleSets) > 0 {
		proxied("rule_set", ruleSetTags(o.DNS.ProxyRuleSets))
	}
	final := tagDNSRemote
	if o.Selective {
		direct("", nil)
		final = tagDNSDirect
	} else {
		// The preset's names get real addresses: with HomeCheck, whether
		// they go direct depends on them.
		if len(o.DNS.HomeSuffixes) > 0 {
			direct("domain_suffix", o.DNS.HomeSuffixes)
		}
		if len(o.DNS.DirectRuleSets) > 0 {
			direct("rule_set", ruleSetTags(o.DNS.DirectRuleSets))
		}
		if o.DNS.FakeIP {
			fakeIP("", nil)
		}
	}

	dns := obj{"servers": servers, "rules": rules, "final": final, "timeout": dnsTimeout}
	if !o.ipv6() {
		dns["strategy"] = "ipv4_only"
	}
	return dns, nil
}

func buildRoute(o Options) obj {
	var rules []any
	if o.RefuseIPv6 {
		// First, ahead of sniffing: sing-box matches rules before a
		// connection is made (PreMatch) only up to a sniff action, which
		// waits for TCP data. Matched here, a TCP connection gets a reset
		// instead of a handshake and a UDP packet an ICMP unreachable, so
		// apps try IPv4 at once, rather than after a connection that
		// seemed to work, or a timeout. DNS over IPv6 (Windows asks the
		// TUN's IPv6 resolver too) is still answered, matched by port
		// since nothing is sniffed yet. no_drop: refusing often must not
		// turn into silently dropping, which would make apps wait.
		rules = append(rules,
			obj{"ip_version": 6, "port": 53, "action": "hijack-dns"},
			obj{"ip_version": 6, "action": "reject", "no_drop": true},
		)
	}
	rules = append(rules,
		obj{"action": "sniff"},
		obj{"protocol": "dns", "action": "hijack-dns"},
	)
	if len(o.BypassProcesses) > 0 {
		rules = append(rules, obj{"process_path": o.BypassProcesses, "outbound": tagDirect})
	}
	if len(o.DNS.BlockSuffixes) > 0 {
		rules = append(rules, obj{"domain_suffix": o.DNS.BlockSuffixes, "action": "reject"})
	}
	if len(o.DirectApps) > 0 {
		if o.DNS.DirectIPv4Only && o.ipv6() {
			// Direct apps with real IPv6 addresses (from their own DNS)
			// cannot reach them without IPv6 of the host's own. Refusing
			// at once makes them fall back to IPv4, instead of every
			// attempt failing as an error in the log. no_drop: rejecting
			// often must not turn into silently dropping, which would
			// make them wait for a timeout.
			rules = append(rules, obj{
				"process_path_regex": appPatterns(o.DirectApps), "ip_cidr": []string{"2000::/3"},
				"action": "reject", "no_drop": true,
			})
		}
		rules = append(rules, obj{"process_path_regex": appPatterns(o.DirectApps), "outbound": tagDirect})
	}
	if len(o.ProxyApps) > 0 {
		rules = append(rules, obj{"process_path_regex": appPatterns(o.ProxyApps), "outbound": tagProxy})
	}
	rules = append(rules, obj{"ip_cidr": lanRanges, "outbound": tagDirect})
	if o.DNS.BlockDoT {
		rules = append(rules, obj{"port": 853, "action": "reject"})
	}
	if o.DNS.BlockBrowserDoH {
		rules = append(rules, obj{"domain_suffix": browserDoHDomains, "outbound": tagProxy})
	}
	// The user's lists: through the tunnel, then around it, wherever the
	// site is.
	if len(o.DNS.ProxySuffixes) > 0 {
		rules = append(rules, obj{"domain_suffix": o.DNS.ProxySuffixes, "outbound": tagProxy})
	}
	if len(o.ProxyIPs) > 0 {
		rules = append(rules, obj{"ip_cidr": prefixStrings(o.ProxyIPs), "outbound": tagProxy})
	}
	if !o.Selective {
		if len(o.DirectIPs) > 0 {
			rules = append(rules, obj{"ip_cidr": prefixStrings(o.DirectIPs), "outbound": tagDirect})
		}
		if o.DNS.DirectIPv4Only && o.ipv6() {
			// Apps can still hold real IPv6 addresses of direct sites, from
			// caches filled before connecting or from their own DoH. Without
			// IPv6 of its own the host cannot reach them; the tunnel can.
			rules = append(rules, obj{"ip_cidr": []string{"2000::/3"}, "outbound": tagProxy})
		}
		if len(o.DNS.DirectSuffixes) > 0 {
			rules = append(rules, obj{"domain_suffix": o.DNS.DirectSuffixes, "outbound": tagDirect})
		}
	}
	// The user's rules by name, in order; then the presets: blocks (ads),
	// Google, the sets of blocked sites, the direct names.
	for _, r := range o.Rules {
		if r.byName() {
			rules = append(rules, r.routeRule())
		}
	}
	if len(o.DNS.BlockRuleSets) > 0 {
		rules = append(rules, obj{"rule_set": ruleSetTags(o.DNS.BlockRuleSets), "action": "reject"})
	}
	if len(o.DNS.PinnedSuffixes) > 0 {
		rules = append(rules, obj{"domain_suffix": o.DNS.PinnedSuffixes, "outbound": tagProxy})
	}
	if len(o.DNS.PinnedRuleSets) > 0 {
		rules = append(rules, obj{"rule_set": ruleSetTags(o.DNS.PinnedRuleSets), "outbound": tagProxy})
	}
	if len(o.DNS.ProxyRuleSets) > 0 {
		rules = append(rules, obj{"rule_set": ruleSetTags(o.DNS.ProxyRuleSets), "outbound": tagProxy})
	}
	if o.Selective {
		return route(o, append(rules, o.addressRules()...), tagDirect)
	}
	preset := obj{}
	if len(o.DNS.HomeSuffixes) > 0 {
		preset["domain_suffix"] = o.DNS.HomeSuffixes
	}
	if len(o.DNS.DirectRuleSets) > 0 {
		preset["rule_set"] = ruleSetTags(o.DNS.DirectRuleSets)
	}
	switch {
	case len(preset) == 0:
	case o.home():
		// A preset's name goes direct where it resolves into the direct
		// address sets (a Russian site hosted in Russia), and through the
		// tunnel otherwise (one behind a foreign CDN or hosting, which the
		// provider may slow down). Its address comes from the direct
		// resolver, as the DNS rules give it to apps, so only a connection
		// by name (a fake address kept from before) costs a direct lookup
		// more. A service hosted abroad that refuses foreign addresses can
		// be listed in DirectSuffixes, which go direct wherever they are.
		resolve := maps.Clone(preset)
		resolve["action"], resolve["server"], resolve["strategy"] = "resolve", tagDNSDirect, "ipv4_only"
		if o.ipv6() && !o.DNS.DirectIPv4Only {
			resolve["strategy"] = "prefer_ipv4"
		}
		abroad := maps.Clone(preset)
		abroad["outbound"] = tagProxy
		rules = append(rules,
			resolve,
			obj{
				"type": "logical", "mode": "and",
				"rules":    []any{preset, obj{"rule_set": ruleSetTags(o.DNS.DirectIPRuleSets)}},
				"outbound": tagDirect,
			},
			abroad,
		)
	default:
		preset["outbound"] = tagDirect
		rules = append(rules, preset)
	}
	return route(o, append(rules, o.addressRules()...), tagProxy)
}

// home reports whether the preset's direct names go direct only where they
// resolve into DirectIPRuleSets (DNSOptions.HomeCheck).
func (o Options) home() bool {
	return !o.Selective && o.DNS.HomeCheck && len(o.DNS.DirectIPRuleSets) > 0
}

// addressRules are the last rules: those of address sets, which need the
// address of a name (Rules of geoip sets, in order, then
// DirectIPRuleSets).
func (o Options) addressRules() []any {
	var rules []any
	for _, r := range o.Rules {
		if !r.byName() {
			rules = append(rules, r.routeRule())
		}
	}
	if !o.Selective && len(o.DNS.DirectIPRuleSets) > 0 {
		rules = append(rules, obj{"rule_set": ruleSetTags(o.DNS.DirectIPRuleSets), "outbound": tagDirect})
	}
	if len(rules) == 0 {
		return nil
	}
	// Through the tunnel's DNS, so looking up the address leaks nothing.
	// The proxy then gets the address instead of the name. A connection by
	// address is left as it is; the preset's names are decided before.
	resolve := obj{"action": "resolve", "server": tagDNSRemote, "strategy": "ipv4_only"}
	if o.ipv6() {
		resolve["strategy"] = "prefer_ipv4"
	}
	return append([]any{resolve}, rules...)
}

func route(o Options, rules []any, final string) obj {
	route := obj{
		"rules":                   rules,
		"final":                   final,
		"auto_detect_interface":   !o.Platform,
		"default_domain_resolver": tagDNSDirect,
	}
	var sets []any
	seen := map[string]bool{}
	for _, rs := range o.ruleSets(false) {
		if seen[rs.Tag] {
			continue
		}
		seen[rs.Tag] = true
		if rs.Path != "" {
			sets = append(sets, obj{"type": "local", "tag": rs.Tag, "format": "binary", "path": rs.Path})
		} else {
			sets = append(sets, obj{"type": "remote", "tag": rs.Tag, "format": "binary", "url": rs.URL})
		}
	}
	if len(sets) > 0 {
		route["rule_set"] = sets
	}
	return route
}

// appPatterns match an executable name at the end of a full path, in any
// case: Windows paths keep whatever case the installer chose.
func appPatterns(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = `(?i)(^|[\\/])` + regexp.QuoteMeta(n) + `$`
	}
	return out
}

func ruleSetTags(sets []RuleSet) []string {
	tags := make([]string, len(sets))
	for i, rs := range sets {
		tags[i] = rs.Tag
	}
	return tags
}

func prefixStrings(ps []netip.Prefix) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}
