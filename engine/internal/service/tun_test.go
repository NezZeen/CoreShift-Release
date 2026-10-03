package service

import (
	"net"
	"net/netip"
	"testing"
)

// The TUN layer counts as up only once its interface has its address: an
// adapter left half registered by an earlier run may be listed without it.
func TestInterfaceHasItsAddress(t *testing.T) {
	ifcs, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifc := range ifcs {
		if ifc.Flags&net.FlagLoopback == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip, _ := netip.AddrFromSlice(n.IP.To4())
			if !interfaceHas(ifc.Name, ip) {
				t.Errorf("%s has %s", ifc.Name, ip)
			}
			if interfaceHas(ifc.Name, netip.MustParseAddr("172.19.0.1")) {
				t.Errorf("%s does not have 172.19.0.1", ifc.Name)
			}
			if interfaceHas("no-such-interface", ip) {
				t.Error("a missing interface has an address")
			}
			return
		}
	}
	t.Skip("no loopback interface with IPv4")
}
