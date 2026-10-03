package service

import (
	"net/netip"
	"strconv"
)

// Linux: the system's resolvers on the local network (tunlayer.Options.
// LANResolvers) stay routed into the TUN so that DNS sent to them is
// hijacked. sing-box routes the whole address, though, so the replies to
// connections that host makes to this machine went into the TUN too and
// died: a VM's or WSL's host is both the resolver and where SSH comes from,
// a home router both the resolver and its own admin page. Two policy rules
// ahead of sing-box's (which start at 9000) narrow that down to DNS: port
// 53 to a resolver looks up sing-box's table, the rest goes by the main
// table as for the rest of the local network.
const (
	resolverDNSPriority  = 8998
	resolverRestPriority = 8999
	// singBoxTable is sing-box's iproute2_table_index, left at its default.
	singBoxTable = 2022
)

// resolverRuleArgs returns the `ip` command lines that add or delete
// (action) the rules for resolvers: those tunlayer carves out of the
// local ranges, i.e. private addresses, without zone or IPv4 mapping.
func resolverRuleArgs(action string, resolvers []netip.Addr) [][]string {
	var out [][]string
	seen := map[netip.Addr]bool{}
	for _, a := range resolvers {
		a = a.WithZone("").Unmap()
		if !a.IsPrivate() || seen[a] {
			continue
		}
		seen[a] = true
		fam := "-4"
		if a.Is6() {
			fam = "-6"
		}
		to := netip.PrefixFrom(a, a.BitLen()).String()
		rule := func(prio int, rest ...string) []string {
			return append([]string{"ip", fam, "rule", action, "priority", strconv.Itoa(prio), "to", to}, rest...)
		}
		table := strconv.Itoa(singBoxTable)
		out = append(out,
			rule(resolverDNSPriority, "ipproto", "udp", "dport", "53", "lookup", table),
			rule(resolverDNSPriority, "ipproto", "tcp", "dport", "53", "lookup", table),
			rule(resolverRestPriority, "lookup", "main"),
		)
	}
	return out
}
