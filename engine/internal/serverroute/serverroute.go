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
	if !ip.IsValid() || ip.IsLoopback() || ip.IsUnspecified() {
		return Route{}, fmt.Errorf("bad server address %s", ip)
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
	r := Route{IP: ip, Gateway: gateway, IfName: ifc.Name, IfIndex: ifc.Index}
	if err := addRoute(r); err != nil {
		return Route{}, err
	}
	return r, nil
}

// Del removes the route. A missing route is not an error.
func Del(r Route) error {
	return delRoute(r)
}
