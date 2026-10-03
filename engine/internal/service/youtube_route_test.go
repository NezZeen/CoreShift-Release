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

// With the Russian preset on, YouTube goes through the tunnel even when a
// video server is a Google Global Cache inside a Russian provider: its
// address is in geoip-ru, which would send it direct, where YouTube is slow
// and its signed links refused. Names the user sends direct stay direct.
func TestYouTubeStaysInTunnelWithRussiaDirect(t *testing.T) {
	ggc := netip.MustParseAddr("198.51.100.10") // a Russian provider's address; in geoip-ru
	google := netip.MustParseAddr("142.250.74.46")
	geoipRU := []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("77.88.0.0/18")}
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
	}
	video := regexp.MustCompile(`googlevideo\.com$`)
	remote := func(d string) netip.Addr {
		switch {
		case video.MatchString(d):
			return ggc
		case suffixMatch(d, []string{"yandex.net", "yandex.ru"}):
			return netip.MustParseAddr("77.88.55.242")
		}
		return google
	}
	build := func(mutate func(*store.Settings)) *router {
		set := store.Defaults()
		set.Routing.RussiaDirect = true
		if mutate != nil {
			mutate(&set)
		}
		o := OptionsFromSettings(set).withDefaults()
		direct, proxied := routeSuffixes(o)
		rs := func(tag string) []tunlayer.RuleSet { return []tunlayer.RuleSet{{Tag: tag, Path: tag + ".srs"}} }
		opts := tunlayer.Options{
			Upstream: netip.MustParseAddrPort("127.0.0.1:17890"), StrictRoute: true, Address6: tunlayer.DefaultAddress6,
			DirectIPs: o.DirectIPs, ProxyIPs: o.ProxyIPs,
			DNS: tunlayer.DNSOptions{
				Remote: o.DNS.Remote, Direct: "192.168.1.1", FakeIP: o.DNS.FakeIP,
				DirectSuffixes: direct, ProxySuffixes: proxied,
				DirectRuleSets: rs("geosite-category-ru"), DirectIPRuleSets: rs("geoip-ru"), ProxyRuleSets: rs("geosite-category-media-ru-blocked"),
			},
		}
		return newRouter(t, opts, sets, remote)
	}

	r := build(nil)
	for _, c := range []struct {
		name string
		c    conn
		want string
	}{
		{"youtube.com", conn{domain: "youtube.com", fakeIP: true, port: 443}, "proxy"},
		{"www.youtube.com", conn{domain: "www.youtube.com", fakeIP: true, port: 443}, "proxy"},
		{"a video server in a Russian provider, by name", conn{domain: "rr1---sn-n8v7znsz.googlevideo.com", fakeIP: true, port: 443}, "proxy"},
		{"i.ytimg.com", conn{domain: "i.ytimg.com", fakeIP: true, port: 443}, "proxy"},
		{"yt3.ggpht.com", conn{domain: "yt3.ggpht.com", fakeIP: true, port: 443}, "proxy"},
		{"youtubei.googleapis.com", conn{domain: "youtubei.googleapis.com", fakeIP: true, port: 443}, "proxy"},
		{"youtu.be", conn{domain: "youtu.be", fakeIP: true, port: 443}, "proxy"},
		// The browser had the address from before connecting: the name
		// sniffed from TLS or QUIC is what keeps it in the tunnel.
		{"a video server in a Russian provider, by address", conn{domain: "rr1---sn-n8v7znsz.googlevideo.com", ip: ggc, port: 443}, "proxy"},
		{"Google, by address", conn{ip: google, port: 443}, "proxy"},
		// Russian sites stay direct.
		{"yandex.ru", conn{domain: "yandex.ru", fakeIP: true, port: 443}, "direct"},
		{"a Russian service off .ru", conn{domain: "mc.yandex.net", fakeIP: true, port: 443}, "direct"},
		{"a Russian address without a name", conn{ip: ggc, port: 443}, "direct"},
		{"blocked media on .ru", conn{domain: "novayagazeta.ru", fakeIP: true, port: 443}, "proxy"},
	} {
		if got := r.route(t, c.c); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}

	// The user's own direct list wins.
	r = build(func(s *store.Settings) { s.Routing.DirectDomains = []string{"googlevideo.com"} })
	if got := r.route(t, conn{domain: "rr1---sn-n8v7znsz.googlevideo.com", fakeIP: true, port: 443}); got != "direct" {
		t.Errorf("googlevideo.com the user sends direct: %s", got)
	}
	if got := r.route(t, conn{domain: "www.youtube.com", fakeIP: true, port: 443}); got != "proxy" {
		t.Errorf("youtube.com with googlevideo.com direct: %s", got)
	}

	// Without the preset nothing is added: everything goes through the tunnel anyway.
	set := store.Defaults()
	set.Routing.RussiaDirect = false
	if _, proxied := routeSuffixes(OptionsFromSettings(set)); len(proxied) != 0 {
		t.Errorf("proxy list without the preset: %v", proxied)
	}
	// In the selective mode the proxy list is the user's alone.
	set.Routing.RussiaDirect, set.Routing.Mode = true, store.RouteSelected
	if _, proxied := routeSuffixes(OptionsFromSettings(set)); len(proxied) != 0 {
		t.Errorf("proxy list in the selective mode: %v", proxied)
	}
}
