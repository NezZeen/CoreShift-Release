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
	// remote is the tunnel's resolver.
	remote func(domain string) netip.Addr
}

func newRouter(t *testing.T, o tunlayer.Options, sets map[string]func(string, netip.Addr) bool, remote func(string) netip.Addr) *router {
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
	return &router{rules: cfg.Route.Rules, final: cfg.Route.Final, sets: sets, remote: remote}
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

func (r *router) route(t *testing.T, c conn) string {
	t.Helper()
	ip := c.ip
	for _, rule := range r.rules {
		matched := true
		for k, v := range rule {
			switch k {
			case "action", "outbound", "server", "strategy", "no_drop":
			case "protocol", "process_path", "process_path_regex":
				matched = false // no DNS here, and apps are not the point
			case "domain_suffix":
				matched = matched && c.domain != "" && suffixMatch(c.domain, strs(v))
			case "ip_cidr":
				ok := false
				for _, p := range strs(v) {
					ok = ok || (ip.IsValid() && netip.MustParsePrefix(p).Contains(ip))
				}
				matched = matched && ok
			case "port":
				matched = matched && int(v.(float64)) == c.port
			case "rule_set":
				ok := false
				for _, tag := range strs(v) {
					f := r.sets[tag]
					if f == nil {
						t.Fatalf("rule set %s has no stand-in", tag)
					}
					ok = ok || f(c.domain, ip)
				}
				matched = matched && ok
			default:
				t.Fatalf("the test does not know %q in %v", k, rule)
			}
		}
		if !matched {
			continue
		}
		switch rule["action"] {
		case "sniff":
		case "resolve":
			// Only a destination that is a name, i.e. a fake address.
			if c.fakeIP {
				ip = r.remote(c.domain)
			}
		case "reject":
			return "reject"
		default:
			return rule["outbound"].(string)
		}
	}
	return r.final
}

// With the Russian preset on, Google, YouTube included, goes through the
// tunnel, with geosite-google or, before it is downloaded, without it: even
// when a server is a Google Global Cache inside a Russian provider, whose
// address is in geoip-ru, which would send it direct, where YouTube is slow
// and its signed links refused. Google's own ranges are not in geoip-ru (see
// rule-set match in the commit), so a connection to them by address goes
// through the tunnel by the final rule: no address list is needed. Names the
// user sends direct stay direct, and Russian sites go direct.
func TestGoogleStaysInTunnelWithRussiaDirect(t *testing.T) {
	ggc := netip.MustParseAddr("198.51.100.10") // a Russian provider's address; in geoipRU below
	google := netip.MustParseAddr("142.250.74.46")
	geoipRU := []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("77.88.0.0/18")}
	// geosite-google has names the built-in list leaves out.
	geositeGoogle := []string{"google.com", "google.ru", "googleapis.com", "gstatic.com", "googlevideo.com", "gvt1.com", "blogger.com", "youtube.com"}
	sets := map[string]func(string, netip.Addr) bool{
		"geosite-category-ru": func(d string, _ netip.Addr) bool { return suffixMatch(d, []string{"yandex.net", "vk.com"}) },
		"geoip-ru": func(_ string, ip netip.Addr) bool {
			for _, p := range geoipRU {
				if ip.IsValid() && p.Contains(ip) {
					return true
				}
			}
			return false
		},
		"geosite-category-media-ru-blocked": func(d string, _ netip.Addr) bool { return suffixMatch(d, []string{"novayagazeta.ru"}) },
		"geosite-google":                    func(d string, _ netip.Addr) bool { return suffixMatch(d, geositeGoogle) },
	}
	cache := regexp.MustCompile(`(googlevideo|gvt1)\.com$`)
	remote := func(d string) netip.Addr {
		switch {
		case cache.MatchString(d):
			return ggc
		case suffixMatch(d, []string{"yandex.net", "yandex.ru"}):
			return netip.MustParseAddr("77.88.55.242")
		}
		return google
	}
	build := func(withSet bool, mutate func(*store.Settings)) *router {
		set := store.Defaults()
		set.Routing.RussiaDirect = true
		if mutate != nil {
			mutate(&set)
		}
		o := OptionsFromSettings(set).withDefaults()
		direct, proxied, first := routeSuffixes(o)
		rs := func(tags ...string) []tunlayer.RuleSet {
			var out []tunlayer.RuleSet
			for _, tag := range tags {
				out = append(out, tunlayer.RuleSet{Tag: tag, Path: tag + ".srs"})
			}
			return out
		}
		proxySets := rs("geosite-category-media-ru-blocked")
		if withSet {
			proxySets = rs("geosite-category-media-ru-blocked", "geosite-google")
		}
		opts := tunlayer.Options{
			Upstream: netip.MustParseAddrPort("127.0.0.1:17890"), StrictRoute: true, Address6: tunlayer.DefaultAddress6,
			DirectIPs: o.DirectIPs, ProxyIPs: o.ProxyIPs,
			DNS: tunlayer.DNSOptions{
				Remote: o.DNS.Remote, Direct: "192.168.1.1", FakeIP: o.DNS.FakeIP,
				DirectSuffixes: direct, ProxySuffixes: proxied, DirectFirst: first,
				DirectRuleSets: rs("geosite-category-ru"), DirectIPRuleSets: rs("geoip-ru"), ProxyRuleSets: proxySets,
			},
		}
		return newRouter(t, opts, sets, remote)
	}
	byName := func(d string) conn { return conn{domain: d, fakeIP: true, port: 443} }

	for _, withSet := range []bool{true, false} {
		r := build(withSet, nil)
		for _, c := range []struct {
			name string
			c    conn
			want string
		}{
			{"google.com", byName("google.com"), "proxy"},
			{"www.google.ru, though .ru goes direct", byName("www.google.ru"), "proxy"},
			{"fonts.gstatic.com", byName("fonts.gstatic.com"), "proxy"},
			{"play.googleapis.com", byName("play.googleapis.com"), "proxy"},
			{"android.clients.google.com", byName("android.clients.google.com"), "proxy"},
			{"mail.google.com", byName("mail.google.com"), "proxy"},
			{"youtube.com", byName("youtube.com"), "proxy"},
			{"www.youtube.com", byName("www.youtube.com"), "proxy"},
			{"a video server in a Russian provider, by name", byName("rr1---sn-n8v7znsz.googlevideo.com"), "proxy"},
			{"i.ytimg.com", byName("i.ytimg.com"), "proxy"},
			{"yt3.ggpht.com", byName("yt3.ggpht.com"), "proxy"},
			{"youtubei.googleapis.com", byName("youtubei.googleapis.com"), "proxy"},
			{"youtu.be", byName("youtu.be"), "proxy"},
			// The app had the address from before connecting: the name
			// sniffed from TLS or QUIC is what keeps it in the tunnel.
			{"a video server in a Russian provider, by address", conn{domain: "rr1---sn-n8v7znsz.googlevideo.com", ip: ggc, port: 443}, "proxy"},
			{"an update server in a Russian provider, by address", conn{domain: "r3---sn-n8v7knez.gvt1.com", ip: ggc, port: 443}, "proxy"},
			// Google's own range is not in geoip-ru: no address list needed.
			{"Google, by address without a name", conn{ip: google, port: 443}, "proxy"},
			// Russian sites stay direct.
			{"yandex.ru", byName("yandex.ru"), "direct"},
			{"a Russian service off .ru", byName("mc.yandex.net"), "direct"},
			{"a Russian address without a name", conn{ip: ggc, port: 443}, "direct"},
			{"blocked media on .ru", byName("novayagazeta.ru"), "proxy"},
		} {
			if got := r.route(t, c.c); got != c.want {
				t.Errorf("geosite-google %v: %s: %s, want %s", withSet, c.name, got, c.want)
			}
		}
		// What only the set has goes through the tunnel with it, and
		// otherwise as any foreign site does.
		if got := r.route(t, byName("www.blogger.com")); got != "proxy" {
			t.Errorf("geosite-google %v: blogger.com: %s", withSet, got)
		}

		// The user's own direct list wins, over the set too.
		r = build(withSet, func(s *store.Settings) { s.Routing.DirectDomains = []string{"googlevideo.com", "maps.google.com"} })
		for d, want := range map[string]string{
			"rr1---sn-n8v7znsz.googlevideo.com": "direct",
			"maps.google.com":                   "direct",
			"www.google.com":                    "proxy",
			"www.youtube.com":                   "proxy",
		} {
			if got := r.route(t, byName(d)); got != want {
				t.Errorf("geosite-google %v, the user's direct list: %s: %s, want %s", withSet, d, got, want)
			}
		}
		// A list that only happens to be wider does not let Google out.
		r = build(withSet, func(s *store.Settings) { s.Routing.DirectDomains = []string{"com"} })
		if got := r.route(t, byName("www.google.com")); got != "proxy" {
			t.Errorf("geosite-google %v, com direct: google.com %s", withSet, got)
		}
	}

	// Without the preset nothing is added: everything goes through the tunnel anyway.
	set := store.Defaults()
	set.Routing.RussiaDirect = false
	if _, proxied, first := routeSuffixes(OptionsFromSettings(set)); len(proxied) != 0 || len(first) != 0 {
		t.Errorf("lists without the preset: %v %v", proxied, first)
	}
	// In the selective mode the proxy list is the user's alone.
	set.Routing.RussiaDirect, set.Routing.Mode = true, store.RouteSelected
	if _, proxied, _ := routeSuffixes(OptionsFromSettings(set)); len(proxied) != 0 {
		t.Errorf("proxy list in the selective mode: %v", proxied)
	}
}
