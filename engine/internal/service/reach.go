package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
)

// When every core fails its checks (supervisor.EventNoBetter) the trouble is
// the server or the network, not a core. Which one is told by looking past
// the tunnel, from the physical network: a TCP handshake with the server's
// own address and port, and with a few well-known hosts. The verdict goes
// into the status and an event, so the UI can say "the server does not
// answer" rather than "maybe the network", and decides whether switching to
// another server can help.

// Reach is that verdict.
type Reach string

const (
	// ReachUnknown: it could not be told (no way around the tunnel, or a
	// server over UDP that ignores pings).
	ReachUnknown Reach = "unknown"
	// ReachServerDown: the server does not answer, while the internet does.
	ReachServerDown Reach = "server-down"
	// ReachOffline: neither the server nor any well-known host answers.
	ReachOffline Reach = "offline"
	// ReachServerUp: the server answers a handshake, yet nothing gets
	// through it: blocked by inspection, its keys or settings changed.
	ReachServerUp Reach = "server-up"
)

// reachHosts answer TCP on 443 from anywhere: Cloudflare, Google and
// Yandex DNS. One answering is enough to call the network up; Yandex is
// there for networks where foreign hosts are blocked.
var reachHosts = []netip.AddrPort{
	netip.MustParseAddrPort("1.1.1.1:443"),
	netip.MustParseAddrPort("8.8.8.8:443"),
	netip.MustParseAddrPort("77.88.8.8:443"),
}

// reachTimeout bounds one whole check.
const reachTimeout = 8 * time.Second

// checkReach tells whether n's server at ip, or the network, is at fault.
// detail says what was seen, for the journal.
func (s *Service) checkReach(ctx context.Context, n node.Node, ip netip.Addr) (r Reach, detail msg.Msg) {
	ctx, cancel := context.WithTimeout(ctx, reachTimeout)
	defer cancel()
	bind, berr := s.cfg.physical()
	// Without the physical interface the hosts would be asked through the
	// tunnel, which is down: only where the app is outside the VPN anyway
	// (Android) the default route does.
	netKnown := berr == nil || s.cfg.AppOutsideVPN
	if berr != nil {
		bind = ping.Bind{}
	}

	var wg sync.WaitGroup
	var serverErr error
	serverKnown := ip.IsValid()
	if serverKnown {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The server's address is routed around the tunnel, so no
			// binding is needed for it; it does not hurt either.
			if overUDP(&n) {
				_, serverErr = s.cfg.icmpPing(ctx, ip, bindFor(bind, ip))
				if errors.Is(serverErr, ping.ErrUnsupported) {
					serverKnown = false
				}
				return
			}
			_, serverErr = s.cfg.tcpPing(ctx, netip.AddrPortFrom(ip, n.Port), bindFor(bind, ip))
		}()
	}
	online := false
	var netErrs []error
	if netKnown {
		var mu sync.Mutex
		for _, h := range reachHosts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.cfg.tcpPing(ctx, h, bindFor(bind, h.Addr()))
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					online = true
				} else {
					netErrs = append(netErrs, fmt.Errorf("%s: %w", h, err))
				}
			}()
		}
	}
	wg.Wait()

	where := fmt.Sprintf("%s:%d", ip, n.Port)
	switch {
	case serverKnown && serverErr == nil:
		return ReachServerUp, msg.New("reach.server_up", "addr", where)
	case netKnown && !online:
		return ReachOffline, msg.New("reach.offline", "errs", netErrsText(netErrs))
	case serverKnown && online:
		return ReachServerDown, msg.New("reach.server_down", "addr", where, "err", netErrText(serverErr))
	case !netKnown:
		return ReachUnknown, msg.New("reach.no_bypass")
	case overUDP(&n):
		return ReachUnknown, msg.New("reach.udp_silent")
	}
	return ReachUnknown, msg.New("reach.unknown")
}

// netErrText says in a few words why a probe failed: a code for the usual
// failures, the error's own text otherwise.
func netErrText(err error) msg.Msg {
	if m, ok := msg.Of(err); ok {
		return m
	}
	var ne net.Error
	low := strings.ToLower(fmt.Sprint(err))
	switch {
	case err == nil:
		return msg.New("net.timeout")
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout(), strings.Contains(low, "timeout"), strings.Contains(low, "timed out"):
		return msg.New("net.timeout")
	case strings.Contains(low, "refused"):
		return msg.New("net.refused")
	case strings.Contains(low, "unreachable"), strings.Contains(low, "no route"):
		return msg.New("net.unreachable")
	case strings.Contains(low, "reset"), strings.Contains(low, "forcibly closed"):
		return msg.New("net.reset")
	}
	return msg.Raw(fmt.Sprint(err))
}

// netErrsText says why each well-known host failed, "1.1.1.1:443 — нет
// ответа; …". The errors start with the host (checkReach).
func netErrsText(errs []error) msg.List {
	type part struct {
		host string
		m    msg.Msg
	}
	parts := make([]part, 0, len(errs))
	for _, e := range errs {
		host, _, _ := strings.Cut(e.Error(), ": ")
		parts = append(parts, part{host, msg.New("net.host_err", "host", host, "err", netErrText(errors.Unwrap(e)))})
	}
	slices.SortFunc(parts, func(a, b part) int { return strings.Compare(a.m.String(), b.m.String()) })
	out := make([]msg.Msg, len(parts))
	for i, p := range parts {
		out[i] = p.m
	}
	return msg.Join("; ", out...)
}

// diagnose runs checkReach for connection gen when every core failed,
// reports the verdict and, where the settings allow, moves to another
// server. One runs at a time.
func (s *Service) diagnose(gen int) {
	s.mu.Lock()
	n, ip := s.lastNode, s.serverIP
	current := gen == s.gen && s.status.State == Connected
	busy := s.diagnosing
	if current && !busy {
		s.diagnosing = true
	}
	s.mu.Unlock()
	if !current || busy {
		return
	}
	if s.noNetwork() {
		// No network at all: the watcher holds the connection, neither the
		// server nor the network beyond is to blame.
		s.mu.Lock()
		s.diagnosing = false
		s.mu.Unlock()
		s.kickNetwork()
		return
	}
	go func() {
		defer func() {
			s.mu.Lock()
			s.diagnosing = false
			s.mu.Unlock()
		}()
		r, detail := s.checkReach(context.Background(), n, ip)
		if (r == ReachOffline || r == ReachUnknown) && s.noNetwork() {
			s.kickNetwork() // the network went away meanwhile
			return
		}
		s.mu.Lock()
		if gen != s.gen || s.status.State != Connected {
			s.mu.Unlock()
			return
		}
		s.status.Problem = string(r)
		s.mu.Unlock()
		s.hub.publish(Event{Kind: "server", Reason: string(r), From: n.Name}.withLine(detail))
		s.failoverSoon(r)
	}()
}

// clearProblem forgets the verdict once the server answers again.
func (s *Service) clearProblem() {
	s.mu.Lock()
	had := s.status.Problem != ""
	s.status.Problem = ""
	name := s.status.Node
	s.mu.Unlock()
	if had {
		s.hub.publish(Event{Kind: "server", Reason: "ok", From: name})
	}
}
