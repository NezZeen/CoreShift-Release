package ping

import (
	"net"
	"slices"
	"strconv"
	"strings"
)

// HasDefaultRoute reports whether the computer has a network at all: a
// default route, IPv4 or IPv6, through an interface that is up and has its
// link, other than those in skip (the engine's own TUN). Without one
// nothing outside the computer can be reached, whatever a VPN does; with
// one the network may still not reach the internet. An error means it
// could not be told.
func HasDefaultRoute(skip ...string) (bool, error) {
	name, err := defaultRouteInterface(skip)
	return name != "", err
}

// DefaultRouteInterface names the interface of the first usable default
// route, as HasDefaultRoute finds it, for the journal to say which network
// the device is on; "" when there is none.
func DefaultRouteInterface(skip ...string) (string, error) { return defaultRouteInterface(skip) }

// usableInterface reports whether a default route through the interface
// counts: up, with its link (a cable unplugged leaves the route behind on
// Linux, marked linkdown), not a loopback and not skipped.
func usableInterface(ifc *net.Interface, skip []string) bool {
	const want = net.FlagUp | net.FlagRunning
	return ifc != nil && ifc.Flags&want == want && ifc.Flags&net.FlagLoopback == 0 && !slices.Contains(skip, ifc.Name)
}

// Route flags of the kernel (linux/route.h, linux/ipv6_route.h).
const (
	rtfUp     = 0x0001
	rtfReject = 0x0200
)

// procDefaultRoute looks for a default route in the kernel's route table
// as /proc/net/route (IPv4) or /proc/net/ipv6_route (v6) prints it, through
// an interface usable accepts. Unreachable and other reject routes, which
// systems add as a last resort, do not count. It returns the route's
// interface, "" when there is none.
func procDefaultRoute(data string, v6 bool, usable func(name string) bool) string {
	for i, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		var iface, flags string
		switch {
		case !v6 && i > 0 && len(f) >= 8:
			// Iface Destination Gateway Flags RefCnt Use Metric Mask ...
			if f[1] != "00000000" || f[7] != "00000000" {
				continue
			}
			iface, flags = f[0], f[3]
		case v6 && len(f) >= 10:
			// dest dest_len src src_len next_hop metric refcnt use flags iface
			if strings.Trim(f[0], "0") != "" || f[1] != "00" {
				continue
			}
			iface, flags = f[9], f[8]
		default:
			continue
		}
		fl, err := strconv.ParseUint(flags, 16, 32)
		if err != nil || fl&rtfUp == 0 || fl&rtfReject != 0 || iface == "lo" {
			continue
		}
		if usable(iface) {
			return iface
		}
	}
	return ""
}
