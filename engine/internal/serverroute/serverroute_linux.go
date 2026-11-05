//go:build linux

package serverroute

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// rulePriority is where the rule sending the server's address to the main
// table sits: before every rule of an auto_route tunnel (sing-box takes the
// first free one below the system's own, 32765 and down) and past the
// kernel's local rule (0). A route alone is not enough: the tunnel's rule
// sends everything to its own table, whose /1 routes would match the server
// before the main table is ever consulted.
const rulePriority = 9000

// netlinkRoute packs r as a netlink host route. The link is looked up by
// name, as interfaces can be renumbered; the index Add recorded is the
// fallback.
func netlinkRoute(r Route) (*netlink.Route, error) {
	idx := r.IfIndex
	if link, err := netlink.LinkByName(r.IfName); err == nil {
		idx = link.Attrs().Index
	} else if idx == 0 {
		return nil, fmt.Errorf("link %s: %w", r.IfName, err)
	}
	return &netlink.Route{
		LinkIndex: idx,
		Dst:       &net.IPNet{IP: r.IP.AsSlice(), Mask: net.CIDRMask(32, 32)},
		Gw:        r.Gateway.AsSlice(),
	}, nil
}

func netlinkRule(r Route) *netlink.Rule {
	rule := netlink.NewRule()
	rule.Priority = rulePriority
	rule.Dst = &net.IPNet{IP: r.IP.AsSlice(), Mask: net.CIDRMask(32, 32)}
	rule.Table = unix.RT_TABLE_MAIN
	return rule
}

func addRoute(r Route) error {
	// The rule first: a route the tunnel's table still shadows would leave
	// the server unreachable between the two.
	if err := netlink.RuleAdd(netlinkRule(r)); err != nil && !errors.Is(err, syscall.EEXIST) {
		return fmt.Errorf("rule: %w", err)
	}
	nl, err := netlinkRoute(r)
	if err != nil {
		return err
	}
	return netlink.RouteReplace(nl)
}

func delRoute(r Route) error {
	// The route first: with it gone the rule would send the server's address
	// to whatever the main table says, which is what the tunnel's own rules
	// do anyway, so the order does not matter for correctness; the rule is
	// removed right after.
	nl, err := netlinkRoute(r)
	if err == nil {
		if err := netlink.RouteDel(nl); err != nil && !gone(err) {
			return err
		}
	}
	if err := netlink.RuleDel(netlinkRule(r)); err != nil && !gone(err) {
		return err
	}
	return nil
}

// gone reports an error that means there was nothing to remove: ESRCH for
// a route, ENOENT for a rule.
func gone(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ENOENT)
}
