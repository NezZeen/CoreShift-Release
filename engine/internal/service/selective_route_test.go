package service

import (
	"net/netip"
	"testing"

	"coreshift/engine/internal/store"
	"coreshift/engine/internal/tunlayer"
)

// In the «Только выбранное» mode everything goes direct unless listed: the
// user's lists and rules still block, send through the tunnel or keep
// direct, by name, by address and by category, and the ad block holds
// unless the user's own rule lets a site through.
func TestSelectiveModeListsAndRules(t *testing.T) {
	set := store.Defaults()
	set.Routing.Mode = store.RouteSelected
	set.Routing.ProxyDomains = []string{"proxy-list.example"}
	set.Routing.ProxyIPs = []string{"198.51.100.200/32"}
	set.Routing.BlockDomains = []string{"blocked-list.example"}
	set.Routing.Rules = []store.Rule{
		{Match: "rule-proxy.example", Action: tunlayer.ActionProxy},
		{Match: "rule-block.example", Action: tunlayer.ActionBlock},
		{Match: "rule-direct.example", Action: tunlayer.ActionDirect},
		{Match: "googlesyndication.com", Action: tunlayer.ActionDirect},
		{Match: "203.0.113.0/24", Action: tunlayer.ActionBlock},
	}
	o := OptionsFromSettings(set).withDefaults()
	if !o.Selective || !o.BlockAds {
		t.Fatalf("selective %v, ad block %v", o.Selective, o.BlockAds)
	}
	rs := func(tag string) *tunlayer.RuleSet { return &tunlayer.RuleSet{Tag: tag, Path: tag + ".srs"} }
	var sets geoRouting
	sets.block = []tunlayer.RuleSet{*rs("geosite-category-ads-all")}
	for _, rule := range o.Rules {
		tr, ok := plainRule(rule)
		if !ok {
			t.Fatalf("rule %+v", rule)
		}
		sets.rules = append(sets.rules, tr)
	}
	// Category rules, as the service makes them from geosite:/geoip:
	// entries once their sets are on disk.
	sets.rules = append(sets.rules,
		tunlayer.Rule{Action: tunlayer.ActionProxy, Set: rs("geosite-ru-blocked")},
		tunlayer.Rule{Action: tunlayer.ActionBlock, Set: rs("geosite-category-media-ru-blocked")},
		tunlayer.Rule{Action: tunlayer.ActionProxy, Set: rs("geoip-ru"), SetIP: true},
	)
	opts := tunlayer.Options{
		Upstream: netip.MustParseAddrPort("127.0.0.1:17890"), StrictRoute: true, Address6: tunlayer.DefaultAddress6,
		DNS: tunlayer.DNSOptions{Remote: o.DNS.Remote, Direct: "192.168.1.1", FakeIP: o.DNS.FakeIP},
	}
	applyRouting(&opts, o, sets)
	r := newRouter(t, opts, presetSets(), lookups)

	byIP := func(a string) conn { return conn{ip: netip.MustParseAddr(a), port: 443} }
	for _, c := range []routeCase{
		{"not listed", byName("unlisted.example"), "direct"},
		{"«Всегда через VPN» site", byName("proxy-list.example"), "proxy"},
		{"«Всегда через VPN» subdomain", byName("cdn.proxy-list.example"), "proxy"},
		{"«Всегда через VPN» address", byIP("198.51.100.200"), "proxy"},
		{"«Блокировать» site", byName("blocked-list.example"), "reject"},
		{"rule → VPN", byName("rule-proxy.example"), "proxy"},
		{"rule → block", byName("rule-block.example"), "reject"},
		{"rule → direct", byName("rule-direct.example"), "direct"},
		{"rule → block by address", byIP("203.0.113.9"), "reject"},
		{"geosite rule → VPN", byName("linkedin.com"), "proxy"},
		{"geosite rule → block", byName("novayagazeta.ru"), "reject"},
		{"geoip rule → VPN, by address", byIP("77.88.55.242"), "proxy"},
		{"geoip rule → VPN, by name", byName("yandex.net"), "proxy"},
		{"ad block", byName("doubleclick.net"), "reject"},
		{"rule → direct over the ad block", byName("googlesyndication.com"), "direct"},
	} {
		if got := r.route(t, c.c); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}
