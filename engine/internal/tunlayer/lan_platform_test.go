package tunlayer

import (
	"net/netip"
	"slices"
	"testing"
)

// Android gets the ranges to leave out of its VpnService: the system's
// resolver and the tunnel's own stay inside, IPv6 only with an IPv6 TUN.
func TestLANRoutes(t *testing.T) {
	o := Options{ExcludeLAN: true, LANResolvers: []netip.Addr{netip.MustParseAddr("192.168.1.1")}}
	routes := LANRoutes(o)
	in := func(s string) bool {
		a := netip.MustParseAddr(s)
		for _, p := range routes {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	for _, s := range []string{"192.168.1.1", DNSAddress(DefaultAddress).String(), "8.8.8.8", "198.18.0.5"} {
		if in(s) {
			t.Errorf("%s left out of the VPN", s)
		}
	}
	for _, s := range []string{"192.168.1.20", "172.25.203.120", "10.0.0.1", "172.19.0.9"} {
		if !in(s) {
			t.Errorf("%s still in the VPN", s)
		}
	}
	for _, p := range routes {
		if p.Addr().Is6() {
			t.Errorf("IPv6 range %s without an IPv6 TUN", p)
		}
	}
	o.Address6 = DefaultAddress6
	if !slices.ContainsFunc(LANRoutes(o), func(p netip.Prefix) bool { return p.Addr().Is6() }) {
		t.Error("no IPv6 ranges with an IPv6 TUN")
	}
	if LANRoutes(Options{}) != nil {
		t.Error("ranges without ExcludeLAN")
	}
}
