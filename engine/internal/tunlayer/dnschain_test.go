package tunlayer

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func addrs(s ...string) []netip.Addr {
	var out []netip.Addr
	for _, a := range s {
		out = append(out, netip.MustParseAddr(a))
	}
	return out
}

func dnsRules(t *testing.T, o Options) (servers, rules []any, final string) {
	t.Helper()
	d := sub(render(t, o), "dns")
	final, _ = d["final"].(string)
	return list(d, "servers"), list(d, "rules"), final
}

// shape returns the DNS rules that have the keys of match (all, if nil), in
// order, as "action:server" for reading a chain at a glance.
func shape(rules []any, match map[string]any) []string {
	var out []string
	prev := false // the rule before is one of those selected
	for _, r := range rules {
		m := r.(map[string]any)
		ok := true
		for k := range match {
			if _, has := m[k]; !has {
				ok = false
			}
		}
		// A respond selects nothing itself: it follows its evaluate.
		ok, prev = ok || (prev && m["action"] == "respond"), ok
		if !ok {
			continue
		}
		switch a, _ := m["action"].(string); a {
		case "evaluate":
			out = append(out, "evaluate:"+m["server"].(string))
		case "respond":
			out = append(out, "respond")
		case "predefined":
			out = append(out, "predefined:"+m["rcode"].(string))
		case "":
			out = append(out, "route:"+m["server"].(string))
		}
	}
	return out
}

func TestNoChainWithoutFallback(t *testing.T) {
	o := baseOptions()
	o.DNS.DirectSuffixes = []string{"ru"}
	servers, rules, _ := dnsRules(t, o)
	for _, r := range rules {
		if a := r.(map[string]any)["action"]; a == "evaluate" || a == "respond" {
			t.Errorf("a rule of a chain without resolvers behind the direct one: %v", r)
		}
	}
	if find(servers, map[string]any{"tag": "network1"}) != nil {
		t.Error("a fallback server without a fallback")
	}
	if got := shape(rules, map[string]any{"domain_suffix": nil}); !slices.Equal(got, []string{"route:direct"}) {
		t.Errorf("direct names = %v, want the direct resolver alone", got)
	}
}

func TestChainAfterDirectNames(t *testing.T) {
	o := baseOptions()
	o.DNS.Direct = "https://94.140.14.14/dns-query"
	o.DNS.DirectFallback = addrs("192.168.1.1", "192.168.1.2")
	o.DNS.DirectSuffixes = []string{"ru"}
	servers, rules, final := dnsRules(t, o)

	// The network's resolvers: plain UDP, dialled directly (no detour).
	for i, a := range []string{"192.168.1.1", "192.168.1.2"} {
		tag := "network" + string(rune('1'+i))
		s := find(servers, map[string]any{"tag": tag})
		if s == nil || s["type"] != "udp" || s["server"] != a || s["detour"] != nil {
			t.Errorf("%s = %v", tag, s)
		}
	}
	// The user's resolver stays as it was.
	if s := find(servers, map[string]any{"tag": "direct"}); s == nil || s["type"] != "https" || s["server"] != "94.140.14.14" {
		t.Errorf("direct = %v", s)
	}

	want := []string{"evaluate:direct", "respond", "evaluate:network1", "respond", "evaluate:network2", "respond", "predefined:SERVFAIL"}
	if got := shape(rules, map[string]any{"domain_suffix": nil}); !slices.Equal(got, want) {
		t.Errorf("direct names' rules = %v, want %v", got, want)
	}
	// Every rule of the chain selects the same names, and only the first
	// resolver is bounded short.
	var tags []string
	for _, r := range rules {
		m := r.(map[string]any)
		if m["action"] == "evaluate" || m["action"] == "respond" || m["rcode"] == "SERVFAIL" {
			if m["action"] == "respond" {
				if m["match_response"] == nil {
					t.Errorf("respond without a tag: %v", m)
				}
			} else if !slices.Equal(toStrings(m["domain_suffix"]), []string{"ru", "lan", "local", "home.arpa"}[:1]) {
				t.Errorf("a rule of the chain selects %v", m["domain_suffix"])
			}
		}
		if m["action"] == "evaluate" {
			tags = append(tags, m["tag"].(string))
			if (m["server"] == "direct") != (m["timeout"] == directAttemptTimeout) {
				t.Errorf("timeout %v on %v", m["timeout"], m["server"])
			}
		}
	}
	// Tags are unique (sing-box refuses repeats) and each is used.
	if len(tags) != 3 || len(slices.Compact(slices.Sorted(slices.Values(tags)))) != 3 {
		t.Errorf("tags = %v", tags)
	}
	used := ""
	for _, r := range rules {
		if t, ok := r.(map[string]any)["match_response"].(string); ok {
			used += t + " "
		}
	}
	for _, tag := range tags {
		if !strings.Contains(used, tag+" ") {
			t.Errorf("tag %s is never answered with", tag)
		}
	}
	// The default is still the tunnel's resolver.
	if final != "remote" {
		t.Errorf("final = %q", final)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, e := range v.([]any) {
		out = append(out, e.(string))
	}
	return out
}

// Names that go through the tunnel never touch the chain: the remote
// resolver has no fallback, which would have to leave the tunnel.
func TestRemoteHasNoFallback(t *testing.T) {
	o := baseOptions()
	o.DNS.FakeIP = false
	o.DNS.Remote = "https://94.140.14.14/dns-query"
	o.DNS.Direct = "tls://1.1.1.1"
	o.DNS.DirectFallback = addrs("192.168.1.1")
	o.DNS.ProxySuffixes = []string{"blocked.example"}
	servers, rules, final := dnsRules(t, o)
	if final != "remote" {
		t.Errorf("final = %q", final)
	}
	if got := shape(rules, map[string]any{"domain_suffix": nil}); !slices.Equal(got, []string{"route:remote"}) {
		t.Errorf("proxied names = %v: only the remote resolver", got)
	}
	remote := find(servers, map[string]any{"tag": "remote"})
	if remote["detour"] != "proxy" {
		t.Errorf("remote = %v", remote)
	}
	for _, s := range servers {
		m := s.(map[string]any)
		if m["tag"] != "remote" && m["tag"] != "fakeip" && m["detour"] != nil {
			t.Errorf("%v goes through the tunnel", m)
		}
	}
}

func TestChainForOwnProcessesAndSelective(t *testing.T) {
	o := baseOptions()
	o.DNS.Direct = "tls://1.1.1.1"
	o.DNS.DirectFallback = addrs("192.168.1.1")
	o.DirectDNSProcesses = []string{`C:\coreshiftd.exe`}
	o.Selective = true
	o.DNS.ProxySuffixes = []string{"youtube.com"}
	_, rules, final := dnsRules(t, o)

	want := []string{"evaluate:direct", "respond", "evaluate:network1", "respond", "predefined:SERVFAIL"}
	if got := shape(rules, map[string]any{"process_path": nil}); !slices.Equal(got, want) {
		t.Errorf("the daemon's lookups = %v, want %v", got, want)
	}
	// Everything not proxied is direct: a chain with no conditions closes the
	// rules, and the final server is only for form.
	last := rules[len(rules)-5:]
	if got := shape(last, nil); !slices.Equal(got, want) {
		t.Errorf("last rules = %v, want %v", got, want)
	}
	for _, r := range last {
		m := r.(map[string]any)
		if m["domain_suffix"] != nil || m["process_path"] != nil {
			t.Errorf("a rule of the catch-all chain has a condition: %v", m)
		}
	}
	if final != "direct" {
		t.Errorf("final = %q", final)
	}
	// Both chains have tags of their own.
	var tags []string
	for _, r := range rules {
		if m := r.(map[string]any); m["action"] == "evaluate" {
			tags = append(tags, m["tag"].(string))
		}
	}
	if len(tags) != 4 || len(slices.Compact(slices.Sorted(slices.Values(tags)))) != 4 {
		t.Errorf("tags = %v", tags)
	}
}

func TestFallbackFiltering(t *testing.T) {
	o := baseOptions()
	o.DNS.Direct = "192.168.1.1"
	o.DNS.DirectSuffixes = []string{"ru"}
	// The direct resolver itself, a link-local address without its
	// interface, a repeat, and more than two.
	o.DNS.DirectFallback = addrs("192.168.1.1", "fe80::1", "192.168.1.2", "192.168.1.2", "10.0.0.1", "10.0.0.2")
	servers, _, _ := dnsRules(t, o)
	var got []string
	for _, s := range servers {
		if m := s.(map[string]any); strings.HasPrefix(m["tag"].(string), "network") {
			got = append(got, m["server"].(string))
		}
	}
	if want := []string{"192.168.1.2", "10.0.0.1"}; !slices.Equal(got, want) {
		t.Errorf("fallback servers = %v, want %v", got, want)
	}

	// Nothing usable left: no chain at all.
	o.DNS.DirectFallback = addrs("192.168.1.1", "fe80::1")
	_, rules, _ := dnsRules(t, o)
	for _, r := range rules {
		if a := r.(map[string]any)["action"]; a == "evaluate" || a == "respond" {
			t.Errorf("a chain with nothing behind the direct resolver: %v", r)
		}
	}
}
