package tunlayer

import (
	"fmt"
	"net/netip"
	"slices"
)

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
