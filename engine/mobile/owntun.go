package mobile

import (
	"net/netip"
	"slices"

	"coreshift/engine/internal/tunlayer"
)

// ownVPN reports a network that is CoreShift's own VPN rather than the
// phone's: its TUN interface (mine, as sing-box registered it) or the
// tunnel's addresses. Android shows the owner of a VPN its own VPN as its
// default network, CoreShift's process left out of the VPN or not, so the
// app's network callback reports the VPN once it is up. Taken for the
// phone's network, its resolver (the tunnel's own, 172.19.0.2) left the
// next connection without a resolver for direct names, and its private
// IPv6 address passed for IPv6 of the phone's own.
func ownVPN(name string, addrs []netip.Prefix, mine []string) bool {
	if name != "" && slices.Contains(mine, name) {
		return true
	}
	own := []netip.Prefix{tunlayer.DefaultAddress.Masked(), tunlayer.DefaultAddress6.Masked()}
	for _, a := range addrs {
		for _, o := range own {
			if o.Contains(a.Addr()) {
				return true
			}
		}
	}
	return false
}
