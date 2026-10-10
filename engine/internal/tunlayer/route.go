package tunlayer

import (
	"maps"
)

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
