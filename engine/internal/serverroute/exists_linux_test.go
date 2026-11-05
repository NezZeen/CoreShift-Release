//go:build linux

package serverroute

import (
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// routeExists reports whether the main table holds r and a rule sends its
// address there.
func routeExists(r Route) bool {
	nl, err := netlinkRoute(r)
	if err != nil {
		return false
	}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, nl, netlink.RT_FILTER_DST|netlink.RT_FILTER_GW|netlink.RT_FILTER_OIF)
	if err != nil || len(routes) == 0 {
		return false
	}
	rules, err := netlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		return false
	}
	for _, rule := range rules {
		if rule.Priority == rulePriority && rule.Table == unix.RT_TABLE_MAIN && rule.Dst != nil && rule.Dst.IP.Equal(r.IP.AsSlice()) {
			return true
		}
	}
	return false
}
