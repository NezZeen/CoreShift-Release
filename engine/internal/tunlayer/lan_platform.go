package tunlayer

import "net/netip"

// LANRoutes are the ranges ExcludeLAN keeps out of the TUN, for a platform
// that owns the routes itself (Android's VpnService, which sing-box cannot
// tell what to leave out): the resolvers carved out as on the desktop, and
// the tunnel's own resolver too, which lies in 172.16.0.0/12 and through
// which the platform sends every lookup. IPv6 ranges only with Address6.
// Nil without ExcludeLAN.
func LANRoutes(o Options) []netip.Prefix {
	if !o.ExcludeLAN {
		return nil
	}
	addr := o.Address
	if !addr.IsValid() {
		addr = DefaultAddress
	}
	resolvers := append([]netip.Addr{DNSAddress(addr)}, o.LANResolvers...)
	var out []netip.Prefix
	for _, p := range excludeLAN(resolvers) {
		if p.Addr().Is6() && !o.Address6.IsValid() {
			continue
		}
		out = append(out, p)
	}
	return out
}
