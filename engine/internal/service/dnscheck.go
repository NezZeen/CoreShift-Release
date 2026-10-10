package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"coreshift/engine/internal/dnsprobe"
	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/tunlayer"
)

// The settings page asks whether the DNS servers the user typed answer
// (POST /v1/dnscheck): the one for direct sites from the physical network,
// the one that lives in the tunnel through the tunnel. A provider that
// blocks a resolver (a white list, a filter on DNS over HTTPS) refuses the
// connection or says nothing, and the result says so, instead of leaving
// the user to find out from sites that do not open.

// DNSCheckRequest names the servers to try, in the settings' own spelling;
// an empty or missing one is not tried.
type DNSCheckRequest struct {
	Direct string `json:"direct"`
	Remote string `json:"remote"`
}

// What a DNSCheckItem's Status is.
const (
	DNSCheckOK      = "ok"
	DNSCheckFail    = "fail"
	DNSCheckSkipped = "skipped"
)

// DNSCheckItem is how one server did.
type DNSCheckItem struct {
	// Status is DNSCheckOK, DNSCheckFail or DNSCheckSkipped (it could not
	// be tried now: Kind says why).
	Status string `json:"status"`
	// MS is the time of the exchange, connection included, when it answered.
	MS int64 `json:"ms,omitempty"`
	// Kind says why not: a dnsprobe.Kind, or "not_connected" and
	// "no_physical" for a server that could not be tried now.
	Kind string `json:"kind,omitempty"`
	// Line says it in Russian; Code and Args in the app's language.
	Line string         `json:"line"`
	Code string         `json:"code"`
	Args map[string]any `json:"args,omitempty"`
}

// DNSCheckResult is the answer for each server asked about.
type DNSCheckResult struct {
	Direct *DNSCheckItem `json:"direct,omitempty"`
	Remote *DNSCheckItem `json:"remote,omitempty"`
}

// dnsProbeTimeout bounds one server's check: long enough for a handshake
// on a poor mobile network, short enough to wait for on a settings page.
const dnsProbeTimeout = 5 * time.Second

// ErrBadDNSServer is a server the settings would not take either.
var ErrBadDNSServer = errors.New("not a DNS server address")

// CheckDNS tries the servers of req. A server that is not an address the
// settings take is an error (ErrBadDNSServer), as is a direct one that is
// not an IP address.
func (s *Service) CheckDNS(ctx context.Context, req DNSCheckRequest) (DNSCheckResult, error) {
	var res DNSCheckResult
	if req.Direct != "" {
		direct, err := tunlayer.ParseDNSServer(req.Direct)
		if err != nil {
			return res, fmt.Errorf("%w: direct: %w", ErrBadDNSServer, err)
		}
		if _, err := netip.ParseAddr(direct.Host); err != nil {
			return res, fmt.Errorf("%w: direct: %q must be an IP address", ErrBadDNSServer, req.Direct)
		}
	}
	if req.Remote != "" {
		if _, err := tunlayer.ParseDNSServer(req.Remote); err != nil {
			return res, fmt.Errorf("%w: remote: %w", ErrBadDNSServer, err)
		}
	}
	var wg sync.WaitGroup
	if req.Direct != "" {
		wg.Go(func() { res.Direct = s.checkDNSServer(ctx, req.Direct, false) })
	}
	if req.Remote != "" {
		wg.Go(func() { res.Remote = s.checkDNSServer(ctx, req.Remote, true) })
	}
	wg.Wait()
	return res, nil
}

func (s *Service) checkDNSServer(ctx context.Context, raw string, tunnel bool) *DNSCheckItem {
	res, skip := s.cfg.dnsProbe(ctx, raw, tunnel)
	return dnsCheckItem(res, skip, tunnel)
}

// dnsCheckItem words a probe's outcome.
func dnsCheckItem(res dnsprobe.Result, skip string, tunnel bool) *DNSCheckItem {
	item := &DNSCheckItem{}
	var m msg.Msg
	switch {
	case skip != "":
		item.Status, item.Kind = DNSCheckSkipped, skip
		m = msg.New("dnscheck.skip." + skip)
	case res.OK:
		item.Status, item.MS = DNSCheckOK, max(res.RTT.Milliseconds(), 1)
		m = msg.New("dnscheck.ok", "ms", item.MS)
	case res.Kind == dnsprobe.KindUnsupported:
		item.Status, item.Kind = DNSCheckSkipped, "unsupported"
		m = msg.New("dnscheck.skip.unsupported")
	default:
		item.Status, item.Kind = DNSCheckFail, string(res.Kind)
		why := whyMsg(res.Kind, res.Err)
		switch {
		case tunnel:
			m = msg.New("dnscheck.remote_fail", "why", why)
		case blockLike(res.Kind):
			m = msg.New("dnscheck.blocked", "why", why)
		default:
			m = msg.New("dnscheck.fail", "why", why)
		}
	}
	item.Line, item.Code, item.Args = m.String(), m.Code, m.Args
	return item
}

// blockLike reports whether a failure is what a provider that blocks a
// resolver looks like: a refused connection, a silence, a cut connection or
// a certificate that is not the server's.
func blockLike(k dnsprobe.Kind) bool {
	switch k {
	case dnsprobe.KindRefused, dnsprobe.KindTimeout, dnsprobe.KindReset, dnsprobe.KindTLS:
		return true
	}
	return false
}

// whyMsg says why a server did not answer.
func whyMsg(k dnsprobe.Kind, detail string) msg.Msg {
	switch k {
	case dnsprobe.KindRefused:
		return msg.New("dns.why.refused")
	case dnsprobe.KindTimeout:
		return msg.New("dns.why.timeout")
	case dnsprobe.KindTLS:
		return msg.New("dns.why.tls")
	case dnsprobe.KindReset:
		return msg.New("dns.why.reset")
	case dnsprobe.KindUnreachable:
		return msg.New("dns.why.unreachable")
	case dnsprobe.KindRcode:
		return msg.New("dns.why.rcode", "code", detail)
	case dnsprobe.KindHTTP:
		return msg.New("dns.why.http", "code", detail)
	}
	if detail == "" {
		detail = "?"
	}
	return msg.Raw(detail)
}

// Why a server could not be tried now.
const (
	skipNotConnected = "not_connected"
	skipNoPhysical   = "no_physical"
)

// probeDNS asks the server raw once: from the physical network, around the
// tunnel, or (tunnel) through the active core, as the tunnel's own resolver
// is reached. skip is why it could not be tried now.
func (s *Service) probeDNS(ctx context.Context, raw string, tunnel bool) (res dnsprobe.Result, skip string) {
	srv, err := tunlayer.ParseDNSServer(raw)
	if err != nil {
		return dnsprobe.Result{Kind: dnsprobe.KindOther, Err: err.Error()}, ""
	}
	var dial dnsprobe.Dial
	if tunnel {
		// Through the core's SOCKS port, which is where the tunnel's
		// resolver is dialled from. Plain DNS over UDP cannot be sent
		// that way here; its TCP port tells whether the resolver is there.
		if s.Status().State != Connected {
			return res, skipNotConnected
		}
		if srv.Type == "udp" {
			srv.Type = "tcp"
		}
		dial, err = dnsprobe.SOCKS5(s.cfg.Listen.String(), s.socks.User, s.socks.Pass, dnsProbeTimeout)
		if err != nil {
			return dnsprobe.Result{Kind: dnsprobe.KindOther, Err: err.Error()}, ""
		}
	} else {
		var ok bool
		if dial, ok = s.physicalDial(srv.Host); !ok {
			return res, skipNoPhysical
		}
	}
	return dnsprobe.Check(ctx, srv, dial, dnsProbeTimeout), ""
}

// physicalDial dials the way the TUN layer's direct resolver is reached:
// out of the physical interface, around the tunnel. ok is false where that
// cannot be had while the tunnel is up: a probe would go through it and say
// nothing of the network.
func (s *Service) physicalDial(host string) (dnsprobe.Dial, bool) {
	bind, err := s.cfg.physical()
	ip, _ := netip.ParseAddr(host)
	if err == nil {
		bind = bindFor(bind, ip)
	} else {
		bind = ping.Bind{}
	}
	// The zero Bind takes the system's routes: right when no tunnel holds
	// them (not connected, or the platform keeps the app outside its VPN),
	// and for a resolver on the machine itself.
	tunnelUp := s.Status().State.active() && s.Options().TUN
	if bind == (ping.Bind{}) && tunnelUp && !s.cfg.AppOutsideVPN && !ip.Unmap().IsLoopback() {
		return nil, false
	}
	d := bind.Dialer(dnsProbeTimeout)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return d.DialContext(ctx, network, addr)
	}, true
}
