//go:build android

package mobile

import (
	"context"
	"errors"
	"net/netip"

	"github.com/sagernet/sing-box/adapter"
	singservice "github.com/sagernet/sing/service"

	mDNS "github.com/miekg/dns"
)

// tunLookup resolves host through the TUN layer's DNS, for the leak test
// (service.Config.TUNLookup). Apps' lookups reach the VpnService's DNS
// address, where sing-box hijacks them into its DNS router; CoreShift
// itself is outside its VPN and cannot send its own that way, so it asks
// the router directly, which applies the same rules: fake addresses,
// the tunnel's resolver or, for direct names, the network's.
func tunLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	i := currentTUN.get()
	if i == nil || i.ctx == nil {
		return nil, errors.New("the TUN layer is not running")
	}
	router := singservice.FromContext[adapter.DNSRouter](i.ctx)
	if router == nil {
		return nil, errors.New("the TUN layer has no DNS router")
	}
	msg := new(mDNS.Msg)
	msg.SetQuestion(mDNS.Fqdn(host), mDNS.TypeA)
	resp, err := router.Exchange(ctx, msg, adapter.DNSQueryOptions{})
	if err != nil {
		return nil, err
	}
	if resp.Rcode != mDNS.RcodeSuccess {
		return nil, errors.New(mDNS.RcodeToString[resp.Rcode])
	}
	var addrs []netip.Addr
	for _, rr := range resp.Answer {
		if a, ok := rr.(*mDNS.A); ok {
			if ip, ok := netip.AddrFromSlice(a.A); ok {
				addrs = append(addrs, ip.Unmap())
			}
		}
	}
	return addrs, nil
}

func (s *tunSlot) get() *vpnInstance {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.i
}
