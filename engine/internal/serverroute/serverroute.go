// Package serverroute manages an OS host route to the VPN server, taking
// that traffic out of the TUN tunnel: the connected server's own address
// goes via the physical interface's default gateway, so it never enters the
// TUN interface. Unlike the route_exclude_address the TUN config carries,
// this route can be swapped when switching servers without restarting the
// TUN layer.
package serverroute

import (
	"fmt"
	"net"
	"net/netip"

	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/tunlayer"
)

// Route is a host route Add made: what Del removes. It JSON-marshals for
// keeping on disk, so a service restarted after a crash can remove a route
// it left behind.
type Route struct {
	IP      netip.Addr `json:"ip"`
	Gateway netip.Addr `json:"gateway"`
	IfName  string     `json:"if_name"`
	IfIndex int        `json:"if_index"`
}

// Add routes ip (a /32 or /128 host route) via the physical interface's
// default gateway (ping.PhysicalRoute) and returns the route made.
// Loopback, unspecified and invalid IPs are an error. IPv6 servers are not
// supported yet: an error (the caller falls back to its old behavior).
func Add(ip netip.Addr) (Route, error) {
	r, err := Want(ip)
	if err != nil {
		return Route{}, err
	}
	if err := addRoute(r); err != nil {
		return Route{}, err
	}
	return r, nil
}

// Want is the route Add would make for ip now, without making it: a held
// route that differs from it leads by a gateway the system no longer uses
// (another Wi-Fi, a cable unplugged), and has to be made again.
func Want(ip netip.Addr) (Route, error) {
	if err := refuse(ip); err != nil {
		return Route{}, err
	}
	if !ip.Is4() {
		return Route{}, fmt.Errorf("IPv6 servers are not supported yet: %s", ip)
	}
	b, gateway, err := ping.PhysicalRoute(tunlayer.DefaultInterface)
	if err != nil {
		return Route{}, err
	}
	ifc, err := net.InterfaceByName(b.Interface)
	if err != nil {
		return Route{}, fmt.Errorf("interface %s: %w", b.Interface, err)
	}
	return Route{IP: ip, Gateway: gateway, IfName: ifc.Name, IfIndex: ifc.Index}, nil
}

// broadcast is the limited broadcast address, which IsMulticast and the like
// do not cover.
var broadcast = netip.AddrFrom4([4]byte{255, 255, 255, 255})

// refuse says why ip cannot be a server's address: a subscription names it,
// and a route to a host's own, multicast or link-local address, or into the
// TUN layer's own ranges (a route there would send the server's traffic
// back into the tunnel it is meant to leave) is never what a server is.
// Private addresses stay: a server on the local network is legitimate.
func refuse(ip netip.Addr) error {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid() || ip.IsLoopback() || ip.IsUnspecified() || ip == broadcast:
		return fmt.Errorf("bad server address %s", ip)
	case ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return fmt.Errorf("bad server address %s: multicast and link-local addresses are not servers", ip)
	case InTunnelRange(ip):
		return fmt.Errorf("server address %s is inside the tunnel's own range; the server would be reached through the tunnel it carries", ip)
	}
	return nil
}

// InTunnelRange reports whether ip is in the address range of the TUN
// interface or the fake-IP pool, which belong to the tunnel layer.
func InTunnelRange(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range []netip.Prefix{tunlayer.DefaultAddress.Masked(), tunlayer.DefaultFakeIPRange, tunlayer.DefaultAddress6.Masked(), tunlayer.DefaultFakeIPRange6} {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Del removes the route. A missing route is not an error.
func Del(r Route) error {
	return delRoute(r)
}
