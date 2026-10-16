package tunlayer

import (
	"fmt"
	"testing"
)

// The network's own names: its domains and names without a dot are asked
// of its resolver and go direct; "wpad" is found nowhere.
func TestLocalNames(t *testing.T) {
	o := baseOptions()
	o.DNS.LocalSuffixes = []string{"corp.example"}
	o.DNS.LocalResolver = addrs("10.1.2.3")[0]
	servers, rules, _ := dnsRules(t, o)
	var local map[string]any
	for _, s := range servers {
		if m := s.(map[string]any); m["tag"] == tagDNSLocal {
			local = m
		}
	}
	if local == nil || local["server"] != "10.1.2.3" {
		t.Fatalf("no server for the network's names: %v", servers)
	}
	var suffix, single, wpad int
	for i, r := range rules {
		m := r.(map[string]any)
		switch {
		case fmt.Sprint(m["domain_suffix"]) == "[corp.example]" && m["server"] == tagDNSLocal && m["action"] == "evaluate":
			suffix = i + 1
		case fmt.Sprint(m["domain_regex"]) == "["+singleLabelPattern+"]" && m["server"] == tagDNSLocal && m["action"] == "evaluate":
			single = i + 1
		case fmt.Sprint(m["domain_regex"]) == "["+wpadPattern+"]" && m["rcode"] == "NXDOMAIN":
			wpad = i + 1
		}
	}
	if suffix == 0 || single == 0 || wpad == 0 || wpad > suffix || wpad > single {
		t.Fatalf("rules: suffix %d, single label %d, wpad %d (refused first):\n%v", suffix, single, wpad, rules)
	}
	// Each answered only with an address in the network.
	for _, i := range []int{suffix, single} {
		next := rules[i].(map[string]any)
		if next["action"] != "respond" || next["ip_is_private"] != true || next["match_response"] != rules[i-1].(map[string]any)["tag"] {
			t.Errorf("rule after %d: %v", i, next)
		}
	}
	// No route rule takes their names direct: a domain the network
	// announced must not lead a public site out of the VPN.
	for _, r := range list(sub(render(t, o), "route"), "rules") {
		if m := r.(map[string]any); fmt.Sprint(m["domain_suffix"]) == "[corp.example]" {
			t.Errorf("a route rule for the network's domains: %v", m)
		}
	}
}

// The direct resolver that is the network's own answers its names itself;
// a link-local resolver without an interface is not used; without one,
// only "wpad" is refused.
func TestLocalNamesResolver(t *testing.T) {
	o := baseOptions()
	o.DNS.LocalSuffixes = []string{"corp.example"}
	o.DNS.LocalResolver = addrs(o.DNS.Direct)[0]
	servers, rules, _ := dnsRules(t, o)
	for _, s := range servers {
		if s.(map[string]any)["tag"] == tagDNSLocal {
			t.Errorf("a second server for the direct resolver: %v", servers)
		}
	}
	if got := shape(rules, map[string]any{"domain_suffix": nil}); fmt.Sprint(got) != "[evaluate:"+tagDNSDirect+" respond]" {
		t.Errorf("suffix rules = %v", got)
	}

	for _, a := range []string{"fe80::1", ""} {
		o := baseOptions()
		o.DNS.LocalSuffixes = []string{"corp.example"}
		if a != "" {
			o.DNS.LocalResolver = addrs(a)[0]
		}
		servers, rules, _ := dnsRules(t, o)
		for _, r := range rules {
			if m := r.(map[string]any); m["domain_suffix"] != nil || fmt.Sprint(m["domain_regex"]) == "["+singleLabelPattern+"]" {
				t.Errorf("%q: a rule for the network's names: %v", a, m)
			}
		}
		for _, s := range servers {
			if s.(map[string]any)["tag"] == tagDNSLocal {
				t.Errorf("%q: a server for the network's names", a)
			}
		}
		refused := false
		for _, r := range rules {
			if fmt.Sprint(r.(map[string]any)["domain_regex"]) == "["+wpadPattern+"]" {
				refused = true
			}
		}
		if !refused {
			t.Errorf("%q: wpad is not refused", a)
		}
	}
}
