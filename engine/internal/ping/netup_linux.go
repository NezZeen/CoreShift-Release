package ping

import (
	"net"
	"os"
)

// anyDefaultRoute reads the kernel's route tables, both families. The
// policy routing table where sing-box puts its own default route is not in
// /proc/net/route, but may be in /proc/net/ipv6_route: the TUN is skipped
// by name.
func anyDefaultRoute(skip []string) (bool, error) {
	usable := func(name string) bool {
		ifc, err := net.InterfaceByName(name)
		return err == nil && usableInterface(ifc, skip)
	}
	v4, err4 := os.ReadFile("/proc/net/route")
	if err4 == nil && procDefaultRoute(string(v4), false, usable) {
		return true, nil
	}
	v6, err6 := os.ReadFile("/proc/net/ipv6_route")
	if err6 == nil && procDefaultRoute(string(v6), true, usable) {
		return true, nil
	}
	if err4 != nil && err6 != nil {
		return false, err4
	}
	return false, nil
}
