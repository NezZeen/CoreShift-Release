package service

import (
	"encoding/json"
	"net/netip"
	"regexp"
	"strings"
	"testing"

	"coreshift/engine/internal/store"
	"coreshift/engine/internal/tunlayer"
)

// conn is a connection as the TUN layer's router sees it: a name it knows
// from a fake address or sniffed from the connection, and the address the
// app dialled (none for a fake one, which the router turns into the name).
type conn struct {
	domain string
	fakeIP bool
	ip     netip.Addr
	port   int
}

// router walks the generated route rules the way sing-box does for the
// rules this package writes, and returns the outbound or action chosen.
type router struct {
	rules []map[string]any
	final string
	// sets says whether a rule set has the name or the address.
	sets map[string]func(domain string, ip netip.Addr) bool
	// lookup is a resolver: the tunnel's ("remote") or the direct one.
	lookup func(server, domain string) netip.Addr
}

func newRouter(t *testing.T, o tunlayer.Options, sets map[string]func(string, netip.Addr) bool, lookup func(server, domain string) netip.Addr) *router {
	t.Helper()
	b, err := tunlayer.Build(o)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
			Final string           `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	return &router{rules: cfg.Route.Rules, final: cfg.Route.Final, sets: sets, lookup: lookup}
}

func strs(v any) []string {
	var out []string
	switch l := v.(type) {
	case []any:
		for _, e := range l {
			out = append(out, e.(string))
		}
	case string:
		out = []string{l}
	}
	return out
}

func suffixMatch(domain string, suffixes []string) bool {
	for _, s := range suffixes {
		if domain == s || strings.HasSuffix(domain, "."+s) {
			return true
		}
	}
	return false
}

// matches reports whether rule's conditions hold for c, whose address is ip.
// Within a rule, names and sets of names or addresses are one group: any of
// them will do (sing-box merges a rule set into the rule's groups).
func (r *router) matches(t *testing.T, rule map[string]any, c conn, ip netip.Addr) bool {
	t.Helper()
	if rule["type"] == "logical" {
		and := rule["mode"] == "and"
		for _, sub := range rule["rules"].([]any) {
			if r.matches(t, sub.(map[string]any), c, ip) != and {
				return !and
			}
		}
		return and
	}
	matched := true
	dest, hasDest := false, false
	for k, v := range rule {
		switch k {
		case "action", "outbound", "server", "strategy", "no_drop", "type", "mode":
		case "protocol", "process_path", "process_path_regex":
			matched = false // no DNS here, and apps are not the point
		case "domain_suffix":
			hasDest = true
			dest = dest || c.domain != "" && suffixMatch(c.domain, strs(v))
		case "ip_cidr":
			hasDest = true
			for _, p := range strs(v) {
				dest = dest || (ip.IsValid() && netip.MustParsePrefix(p).Contains(ip))
			}
		case "rule_set":
			hasDest = true
			for _, tag := range strs(v) {
				f := r.sets[tag]
				if f == nil {
					t.Fatalf("rule set %s has no stand-in", tag)
				}
				dest = dest || f(c.domain, ip)
			}
		case "port":
			matched = matched && int(v.(float64)) == c.port
		default:
			t.Fatalf("the test does not know %q in %v", k, rule)
		}
	}
	return matched && (!hasDest || dest)
}

func (r *router) route(t *testing.T, c conn) string {
	t.Helper()
	ip := c.ip
	for _, rule := range r.rules {
		if !r.matches(t, rule, c, ip) {
			continue
		}
		switch rule["action"] {
		case "sniff":
		case "resolve":
			// Only a destination that is a name, i.e. a fake address.
			if c.fakeIP {
				ip = r.lookup(rule["server"].(string), c.domain)
			}
		case "reject":
			return "reject"
		default:
			return rule["outbound"].(string)
		}
	}
	return r.final
}

// The sets of the presets and the addresses the resolvers give, as the
// tests below see them.
var (
	ggc       = netip.MustParseAddr("198.51.100.10") // a Russian provider's address; in geoipRU below
	googleIP  = netip.MustParseAddr("142.250.74.46")
	russianIP = netip.MustParseAddr("77.88.55.242")
	foreignIP = netip.MustParseAddr("104.21.1.1") // a foreign CDN
	geoipRU   = []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("77.88.0.0/18")}
	// geosite-google has names the built-in list leaves out.
	geositeGoogle = []string{"google.com", "google.ru", "googleapis.com", "gstatic.com", "googlevideo.com", "gvt1.com", "blogger.com", "youtube.com"}
)

func presetSets() map[string]func(string, netip.Addr) bool {
	names := func(list ...string) func(string, netip.Addr) bool {
		return func(d string, _ netip.Addr) bool { return suffixMatch(d, list) }
	}
	return map[string]func(string, netip.Addr) bool{
		"geosite-category-ru": names("yandex.net", "vk.com", "ozon.example"),
		"geoip-ru": func(_ string, ip netip.Addr) bool {
			for _, p := range geoipRU {
				if ip.IsValid() && p.Contains(ip) {
					return true
				}
			}
			return false
		},
		"geosite-category-media-ru-blocked": names("novayagazeta.ru"),
		"geosite-ru-blocked":                names("blocked-shop.ru", "linkedin.com"),
		"geosite-google":                    names(geositeGoogle...),
		"geosite-category-ads-all":          names("doubleclick.net", "googlesyndication.com", "ads.example"),
	}
}

// lookups: video and update caches inside a Russian provider, Yandex in
// Russia, .ru shops behind a foreign CDN, Google elsewhere.
func lookups(_, d string) netip.Addr {
	switch {
	case regexp.MustCompile(`(googlevideo|gvt1)\.com$`).MatchString(d):
		return ggc
	case suffixMatch(d, []string{"yandex.net", "yandex.ru", "ozon.example", "gosuslugi.ru"}):
		return russianIP
	case strings.HasSuffix(d, ".ru"):
		return foreignIP
	}
	return googleIP
}

// presetRouter routes as a connection with set does, with the preset's
// sets on disk (or without geosite-google).
func presetRouter(t *testing.T, set store.Settings, withGoogle bool) *router {
	t.Helper()
	o := OptionsFromSettings(set).withDefaults()
	rs := func(tag string) []tunlayer.RuleSet { return []tunlayer.RuleSet{{Tag: tag, Path: tag + ".srs"}} }
	var sets geoRouting
	if o.DNS.RussiaDirect && !o.Selective {
		sets.domain, sets.ip = rs("geosite-category-ru"), rs("geoip-ru")
		sets.proxied = append(rs("geosite-category-media-ru-blocked"), rs("geosite-ru-blocked")...)
		if withGoogle {
			sets.pinned = rs("geosite-google")
		}
	}
	if o.BlockAds {
		sets.block = rs("geosite-category-ads-all")
	}
	for _, rule := range o.Rules {
		if tr, ok := plainRule(rule); ok {
			sets.rules = append(sets.rules, tr)
		}
	}
	opts := tunlayer.Options{
		Upstream: netip.MustParseAddrPort("127.0.0.1:17890"), StrictRoute: true, Address6: tunlayer.DefaultAddress6,
		DNS: tunlayer.DNSOptions{Remote: o.DNS.Remote, Direct: "192.168.1.1", FakeIP: o.DNS.FakeIP},
	}
	applyRouting(&opts, o, sets)
	return newRouter(t, opts, presetSets(), lookups)
}

func byName(d string) conn { return conn{domain: d, fakeIP: true, port: 443} }

// The preset's names resolve directly to real addresses: apps connect by
// them, with the name sniffed.
func byReal(d string) conn { return conn{domain: d, ip: lookups("direct", d), port: 443} }

type routeCase struct {
	name string
	c    conn
	want string
}

func checkRoutes(t *testing.T, what string, r *router, cases []routeCase) {
	t.Helper()
	for _, c := range cases {
		if got := r.route(t, c.c); got != c.want {
			t.Errorf("%s: %s: %s, want %s", what, c.name, got, c.want)
		}
	}
}

func russianPreset(mutate func(*store.Settings)) store.Settings {
	set := store.Defaults()
	set.Routing.RussiaDirect = true
	if mutate != nil {
		mutate(&set)
	}
	return set
}

// With the Russian preset on, Google, YouTube included, goes through the
// tunnel, with geosite-google or, before it is downloaded, without it: even
// when a server is a Google Global Cache inside a Russian provider, whose
// address is in geoip-ru, which would send it direct, where YouTube is slow
// and its signed links refused. Google's own ranges are not in geoip-ru (see
// rule-set match in the commit), so a connection to them by address goes
// through the tunnel by the final rule: no address list is needed. Only the
// user's own lists come first, and the ad block: Google's ad servers are
// refused.
func TestGoogleStaysInTunnelWithRussiaDirect(t *testing.T) {
	for _, withGoogle := range []bool{true, false} {
		r := presetRouter(t, russianPreset(nil), withGoogle)
		checkRoutes(t, map[bool]string{true: "with geosite-google", false: "without geosite-google"}[withGoogle], r, []routeCase{
			{"google.com", byName("google.com"), "proxy"},
			{"www.google.ru, though .ru goes direct", byName("www.google.ru"), "proxy"},
			{"fonts.gstatic.com", byName("fonts.gstatic.com"), "proxy"},
			{"play.googleapis.com", byName("play.googleapis.com"), "proxy"},
			{"mail.google.com", byName("mail.google.com"), "proxy"},
			{"youtube.com", byName("youtube.com"), "proxy"},
			{"a video server in a Russian provider, by name", byName("rr1---sn-n8v7znsz.googlevideo.com"), "proxy"},
			{"i.ytimg.com", byName("i.ytimg.com"), "proxy"},
			{"youtu.be", byName("youtu.be"), "proxy"},
			// The app had the address from before connecting: the name
			// sniffed from TLS or QUIC is what keeps it in the tunnel.
			{"a video server in a Russian provider, by address", conn{domain: "rr1---sn-n8v7znsz.googlevideo.com", ip: ggc, port: 443}, "proxy"},
			{"an update server in a Russian provider, by address", conn{domain: "r3---sn-n8v7knez.gvt1.com", ip: ggc, port: 443}, "proxy"},
			{"Google, by address without a name", conn{ip: googleIP, port: 443}, "proxy"},
			// Ads, Google's too, are refused.
			{"doubleclick.net", byName("doubleclick.net"), "reject"},
			{"googlesyndication.com", byName("pagead2.googlesyndication.com"), "reject"},
			// Russian sites hosted in Russia stay direct.
			{"yandex.ru", byReal("yandex.ru"), "direct"},
			{"a Russian service off .ru", byReal("mc.yandex.net"), "direct"},
			{"a Russian address without a name", conn{ip: ggc, port: 443}, "direct"},
			{"blocked media on .ru", byName("novayagazeta.ru"), "proxy"},
			{"a blocked site on .ru", byReal("blocked-shop.ru"), "proxy"},
		})
		// What only the set has goes through the tunnel with it, and
		// otherwise as any foreign site does.
		if got := r.route(t, byName("www.blogger.com")); got != "proxy" {
			t.Errorf("geosite-google %v: blogger.com: %s", withGoogle, got)
		}

		// The user's own direct list wins: over Google and the ad block.
		r = presetRouter(t, russianPreset(func(s *store.Settings) {
			s.Routing.DirectDomains = []string{"youtube.com", "doubleclick.net"}
		}), withGoogle)
		checkRoutes(t, "the user's direct list", r, []routeCase{
			{"youtube.com", byName("www.youtube.com"), "direct"},
			{"doubleclick.net", byName("doubleclick.net"), "direct"},
			{"google.com", byName("www.google.com"), "proxy"},
			{"googlesyndication.com", byName("googlesyndication.com"), "reject"},
		})
		// ... and the user's rules, after the lists.
		r = presetRouter(t, russianPreset(func(s *store.Settings) {
			s.Routing.ProxyDomains = []string{"yandex.ru"}
			s.Routing.Rules = []store.Rule{{Match: "ads.example", Action: store.RuleDirect}, {Match: "yandex.ru", Action: store.RuleBlock}}
		}), withGoogle)
		checkRoutes(t, "the user's rules", r, []routeCase{
			{"the ad block", byName("ads.example"), "direct"},
			{"a rule after the user's list", byReal("yandex.ru"), "proxy"},
		})
	}

	// Without the ad block, Google's ad servers stay in the tunnel.
	r := presetRouter(t, russianPreset(func(s *store.Settings) { s.Routing.BlockAds = false }), true)
	if got := r.route(t, byName("doubleclick.net")); got != "proxy" {
		t.Errorf("doubleclick.net without the ad block: %s", got)
	}
	// Without the preset nothing is pinned: everything goes through the
	// tunnel anyway. In the selective mode the proxy list is the user's
	// alone.
	set := store.Defaults()
	if _, proxied, pinned, home := routeSuffixes(OptionsFromSettings(set)); len(proxied)+len(pinned)+len(home) != 0 {
		t.Errorf("lists without the preset: %v %v %v", proxied, pinned, home)
	}
	set.Routing.RussiaDirect, set.Routing.Mode = true, store.RouteSelected
	if _, proxied, pinned, _ := routeSuffixes(OptionsFromSettings(set)); len(proxied)+len(pinned) != 0 {
		t.Errorf("proxy list in the selective mode: %v %v", proxied, pinned)
	}
	// The ad block works in the selective mode too.
	r = presetRouter(t, set, true)
	checkRoutes(t, "selective", r, []routeCase{
		{"doubleclick.net", byName("doubleclick.net"), "reject"},
		{"yandex.ru", byReal("yandex.ru"), "direct"},
		{"google.com", byReal("google.com"), "direct"},
	})
}

// A Russian site goes direct where its server is in Russia, and through the
// tunnel where it is abroad, unless the user switches that off or lists the
// site to go direct.
func TestRussianSitesAbroad(t *testing.T) {
	r := presetRouter(t, russianPreset(func(s *store.Settings) { s.Routing.DirectDomains = []string{"bank.ru"} }), true)
	checkRoutes(t, "abroad through the tunnel", r, []routeCase{
		{"a .ru site in Russia, by its real address", byReal("gosuslugi.ru"), "direct"},
		{"a .ru site in Russia, by name", byName("gosuslugi.ru"), "direct"},
		{"a .ru site abroad, by its real address", byReal("shop.ru"), "proxy"},
		{"a .ru site abroad, by name", byName("shop.ru"), "proxy"},
		{"a Russian service off .ru, in Russia", byReal("ozon.example"), "direct"},
		{"the user's direct site abroad", byReal("online.bank.ru"), "direct"},
		{"2ip.io, to check the preset", byReal("2ip.io"), "direct"},
	})
	r = presetRouter(t, russianPreset(func(s *store.Settings) { s.Routing.RussiaAbroad = false }), true)
	checkRoutes(t, "by name", r, []routeCase{
		{"a .ru site abroad", byReal("shop.ru"), "direct"},
		{"a .ru site abroad, by name", byName("shop.ru"), "direct"},
		{"a blocked site on .ru", byReal("blocked-shop.ru"), "proxy"},
	})
}
