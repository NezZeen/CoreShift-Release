package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"time"

	"coreshift/engine/internal/tunlayer"
)

// serverAddr returns the server as an IP, resolving a hostname with the
// system resolver.
func (s *Service) serverAddr(ctx context.Context, host string) (netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return a, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s.mu.Lock()
	server := s.tunDNS
	s.mu.Unlock()
	a, err := s.cfg.lookup(ctx, host, server)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve server %s: %w", host, err)
	}
	return a, nil
}

func lookupHost(ctx context.Context, host string, server netip.AddrPort) (netip.Addr, error) {
	r := net.DefaultResolver
	if server.IsValid() {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, server.String())
			},
		}
	}
	ips, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, err
	}
	// Prefer IPv4: the TUN layer is IPv4-only by default.
	for _, ip := range ips {
		if ip.Unmap().Is4() {
			return ip.Unmap(), nil
		}
	}
	if len(ips) == 0 {
		return netip.Addr{}, errors.New("no addresses")
	}
	return ips[0], nil
}

func mergeSuffixes(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		for _, v := range l {
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// hostHasIPv6 reports whether the computer has a public IPv6 address of its
// own, i.e. whether it can reach IPv6 hosts without the tunnel.
// relayedIPv6 are addresses of IPv6-over-IPv4 relays, Teredo and 6to4.
// Windows keeps a Teredo address on hosts without IPv6 of their own, and
// such a relay reaches hardly any sites.
var relayedIPv6 = []netip.Prefix{netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2002::/16")}

// nativeIPv6 reports whether ip is a global IPv6 address that means the
// host has IPv6 of its own.
func nativeIPv6(ip netip.Addr) bool {
	if !ip.Is6() || ip.Is4In6() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	return !slices.ContainsFunc(relayedIPv6, func(p netip.Prefix) bool { return p.Contains(ip) })
}

func hostHasIPv6() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Name == tunlayer.DefaultInterface {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			p, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := p.Addr()
			if nativeIPv6(ip) {
				return true
			}
		}
	}
	return false
}
