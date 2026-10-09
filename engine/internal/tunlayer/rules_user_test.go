package tunlayer

import (
	"net/netip"
	"strings"
	"testing"
)

func userRulesOptions() Options {
	o := baseOptions()
	o.DirectApps = []string{"qbittorrent.exe"}
	o.DNS.BlockSuffixes = []string{"blocked.example"}
	o.DNS.ProxySuffixes = []string{"user-proxy.example"}
	o.DNS.DirectSuffixes = []string{"lan", "user-direct.example"}
	o.DNS.BlockRuleSets = []RuleSet{{Tag: "geosite-category-ads-all", Path: "/rules/ads.srs"}}
	o.DNS.PinnedSuffixes = []string{"google.com"}
	o.DNS.PinnedRuleSets = []RuleSet{{Tag: "geosite-google", Path: "/rules/google.srs"}}
	o.DNS.ProxyRuleSets = []RuleSet{{Tag: "geosite-ru-blocked", Path: "/rules/blocked.srs"}}
	o.DNS.HomeSuffixes = []string{"ru"}
	o.DNS.DirectIPRuleSets = []RuleSet{{Tag: "geoip-ru", Path: "/rules/geoip-ru.srs"}}
	o.Rules = []Rule{
		{Action: ActionDirect, Set: &RuleSet{Tag: "user-geosite-youtube", Path: "/rules/yt.srs"}},
		{Action: ActionProxy, Set: &RuleSet{Tag: "user-geoip-ru", Path: "/rules/u-geoip-ru.srs"}, SetIP: true},
		{Action: ActionBlock, Domains: []string{"tracker.example"}},
		{Action: ActionDirect, IPs: []netip.Prefix{netip.MustParsePrefix("10.8.0.0/16")}},
		{Action: ActionBlock, Set: &RuleSet{Tag: "user-geoip-cn", Path: "/rules/cn.srs"}, SetIP: true},
		// The same set as a preset's is defined once.
		{Action: ActionBlock, Set: &RuleSet{Tag: "geosite-category-ads-all", Path: "/rules/ads.srs"}},
	}
	return o
}

func TestUserRulesOrder(t *testing.T) {
	cfg := render(t, userRulesOptions())
	rules := list(sub(cfg, "route"), "rules")
	at := func(m map[string]any) int {
		t.Helper()
		i := ruleIndex(rules, m)
		if i < 0 {
			t.Fatalf("no rule %v in %v", m, rules)
		}
		return i
	}
	// The user's lists, the user's rules by name in order, the presets,
	// the rules of address sets, the preset's addresses.
	order := []int{
		at(map[string]any{"domain_suffix": []any{"blocked.example"}, "action": "reject"}),
		at(map[string]any{"outbound": "direct", "process_path_regex": []any{`(?i)(^|[\\/])qbittorrent\.exe$`}}),
		at(map[string]any{"domain_suffix": []any{"user-proxy.example"}, "outbound": "proxy"}),
		at(map[string]any{"domain_suffix": []any{"lan", "user-direct.example"}, "outbound": "direct"}),
		at(map[string]any{"rule_set": []any{"user-geosite-youtube"}, "outbound": "direct"}),
		at(map[string]any{"domain_suffix": []any{"tracker.example"}, "action": "reject"}),
		at(map[string]any{"ip_cidr": []any{"10.8.0.0/16"}, "outbound": "direct"}),
		at(map[string]any{"rule_set": []any{"geosite-category-ads-all"}, "action": "reject"}),
		at(map[string]any{"rule_set": []any{"geosite-google"}, "outbound": "proxy"}),
		at(map[string]any{"rule_set": []any{"geosite-ru-blocked"}, "outbound": "proxy"}),
		at(map[string]any{"domain_suffix": []any{"ru"}, "outbound": "direct"}),
		at(map[string]any{"action": "resolve", "server": "remote"}),
		at(map[string]any{"rule_set": []any{"user-geoip-ru"}, "outbound": "proxy"}),
		at(map[string]any{"rule_set": []any{"user-geoip-cn"}, "action": "reject"}),
		at(map[string]any{"rule_set": []any{"geoip-ru"}, "outbound": "direct"}),
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] >= order[i] {
			t.Fatalf("rule order %v:\n%v", order, rules)
		}
	}

	n := 0
	for _, rs := range list(sub(cfg, "route"), "rule_set") {
		if rs.(map[string]any)["tag"] == "geosite-category-ads-all" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the ad set defined %d times", n)
	}

	// DNS in the same order; the address rules cannot decide a server.
	dns := list(sub(cfg, "dns"), "rules")
	dnsOrder := []int{
		ruleIndex(dns, map[string]any{"domain_suffix": []any{"user-proxy.example"}, "server": "remote"}),
		ruleIndex(dns, map[string]any{"domain_suffix": []any{"lan", "user-direct.example"}, "server": "direct"}),
		ruleIndex(dns, map[string]any{"rule_set": []any{"user-geosite-youtube"}, "server": "direct"}),
		ruleIndex(dns, map[string]any{"domain_suffix": []any{"tracker.example"}, "rcode": "NXDOMAIN"}),
		ruleIndex(dns, map[string]any{"rule_set": []any{"geosite-category-ads-all"}, "rcode": "NXDOMAIN"}),
		ruleIndex(dns, map[string]any{"rule_set": []any{"geosite-google"}, "server": "remote"}),
		ruleIndex(dns, map[string]any{"domain_suffix": []any{"ru"}, "server": "direct"}),
	}
	for i := range dnsOrder {
		if dnsOrder[i] < 0 || i > 0 && dnsOrder[i-1] >= dnsOrder[i] {
			t.Fatalf("DNS order %v:\n%v", dnsOrder, dns)
		}
	}
	if ruleIndex(dns, map[string]any{"rule_set": []any{"user-geoip-ru"}}) >= 0 || ruleIndex(dns, map[string]any{"ip_cidr": []any{"10.8.0.0/16"}}) >= 0 {
		t.Error("an address rule selects a DNS server")
	}

	// Selective: the rules and the ad block work there too.
	o := userRulesOptions()
	o.Selective = true
	route := sub(render(t, o), "route")
	rules = list(route, "rules")
	if route["final"] != "direct" || ruleIndex(rules, map[string]any{"rule_set": []any{"user-geoip-ru"}, "outbound": "proxy"}) < 0 ||
		ruleIndex(rules, map[string]any{"rule_set": []any{"geosite-category-ads-all"}, "action": "reject"}) < 0 ||
		ruleIndex(rules, map[string]any{"rule_set": []any{"geoip-ru"}}) >= 0 {
		t.Errorf("selective: %v", rules)
	}
}

func TestUserRulesValidation(t *testing.T) {
	cases := map[string]func(*Options){
		"no match": func(o *Options) { o.Rules = []Rule{{Action: ActionBlock}} },
		"two matches": func(o *Options) {
			o.Rules = []Rule{{Action: ActionBlock, Domains: []string{"a.example"}, IPs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}}
		},
		"bad action":  func(o *Options) { o.Rules = []Rule{{Action: "allow", Domains: []string{"a.example"}}} },
		"set no path": func(o *Options) { o.Rules = []Rule{{Action: ActionBlock, Set: &RuleSet{Tag: "x"}}} },
		"ip, no set":  func(o *Options) { o.Rules = []Rule{{Action: ActionBlock, SetIP: true, Domains: []string{"a.example"}}} },
		"tag clashes": func(o *Options) {
			o.Rules = []Rule{{Action: ActionBlock, Set: &RuleSet{Tag: "geoip-ru", Path: "/other.srs"}}}
		},
	}
	for name, mutate := range cases {
		o := userRulesOptions()
		mutate(&o)
		if _, err := Build(o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestHomeCheck(t *testing.T) {
	o := baseOptions()
	o.Address6 = DefaultAddress6
	o.DNS.DirectSuffixes = []string{"lan", "bank.example"}
	o.DNS.HomeSuffixes = []string{"ru", "su"}
	o.DNS.HomeCheck = true
	o.DNS.DirectRuleSets = []RuleSet{{Tag: "geosite-category-ru", Path: "/rules/ru.srs"}}
	o.DNS.DirectIPRuleSets = []RuleSet{{Tag: "geoip-ru", Path: "/rules/geoip-ru.srs"}}
	cfg := render(t, o)
	rules := list(sub(cfg, "route"), "rules")
	home := map[string]any{"domain_suffix": []any{"ru", "su"}, "rule_set": []any{"geosite-category-ru"}}
	user := ruleIndex(rules, map[string]any{"domain_suffix": []any{"lan", "bank.example"}, "outbound": "direct"})
	resolve := ruleIndex(rules, map[string]any{"domain_suffix": []any{"ru", "su"}, "action": "resolve", "server": "direct", "strategy": "prefer_ipv4"})
	and := ruleIndex(rules, map[string]any{"type": "logical", "mode": "and", "outbound": "direct",
		"rules": []any{home, map[string]any{"rule_set": []any{"geoip-ru"}}}})
	abroad := ruleIndex(rules, map[string]any{"domain_suffix": []any{"ru", "su"}, "rule_set": []any{"geosite-category-ru"}, "outbound": "proxy"})
	remote := ruleIndex(rules, map[string]any{"action": "resolve", "server": "remote"})
	if user < 0 || !(user < resolve && resolve < and && and < abroad && abroad < remote) {
		t.Fatalf("order: user %d, resolve %d, and %d, abroad %d, remote %d in %v", user, resolve, and, abroad, remote, rules)
	}
	// Never by name alone.
	if ruleIndex(rules, map[string]any{"domain_suffix": []any{"ru", "su"}, "outbound": "direct"}) >= 0 {
		t.Error("the preset's names go direct by name")
	}
	// Their real addresses come from the direct resolver.
	dns := list(sub(cfg, "dns"), "rules")
	if ruleIndex(dns, map[string]any{"domain_suffix": []any{"ru", "su"}, "server": "direct"}) < 0 ||
		ruleIndex(dns, map[string]any{"rule_set": []any{"geosite-category-ru"}, "server": "direct"}) < 0 {
		t.Errorf("the preset's names not resolved directly: %v", dns)
	}

	// Without the check, or without an address set to check against: by
	// name, as before.
	for _, mutate := range []func(*Options){
		func(o *Options) { o.DNS.HomeCheck = false },
		func(o *Options) { o.DNS.DirectIPRuleSets = nil },
	} {
		o2 := o
		mutate(&o2)
		b, err := Build(o2)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"logical"`) || ruleIndex(list(sub(render(t, o2), "route"), "rules"),
			map[string]any{"domain_suffix": []any{"ru", "su"}, "rule_set": []any{"geosite-category-ru"}, "outbound": "direct"}) < 0 {
			t.Errorf("by name: %s", b)
		}
	}
}
