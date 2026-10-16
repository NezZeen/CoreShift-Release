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
		case fmt.Sprint(m["domain_suffix"]) == "[corp.example]" && m["server"] == tagDNSLocal:
			suffix = i + 1
		case fmt.Sprint(m["domain_regex"]) == "["+singleLabelPattern+"]" && m["server"] == tagDNSLocal:
			single = i + 1
		case fmt.Sprint(m["domain_regex"]) == "["+wpadPattern+"]" && m["rcode"] == "NXDOMAIN":
			wpad = i + 1
		}
	}
	if suffix == 0 || single == 0 || wpad == 0 || wpad > suffix || wpad > single {
		t.Errorf("rules: suffix %d, single label %d, wpad %d (refused first):\n%v", suffix, single, wpad, rules)
	}
	direct := false
	for _, r := range list(sub(render(t, o), "route"), "rules") {
		m := r.(map[string]any)
		if fmt.Sprint(m["domain_suffix"]) == "[corp.example]" && m["outbound"] == tagDirect {
			direct = true
		}
	}
	if !direct {
		t.Error("the network's names do not go direct")
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
	if got := shape(rules, map[string]any{"domain_suffix": nil}); fmt.Sprint(got) != "[route:"+tagDNSDirect+"]" {
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
