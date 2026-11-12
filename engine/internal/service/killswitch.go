package service

import (
	"cmp"
	"context"
	"errors"
	"net/netip"
	"time"

	"coreshift/engine/internal/msg"
)

// The kill switch («Блокировать трафик при сбое», Options.KillSwitch).
//
// Without it a connection that fails (every core dropped, the TUN layer
// died, a switch of servers found no working one) is taken down whole: the
// TUN layer goes, the system gets its DNS back, and what the programs send
// leaves by the real address until the user notices. With it the failure
// takes down the cores only. The TUN layer and the DNS redirect stay, which
// is what keeps the traffic in: with no core behind it the layer's gate
// holds a connection for a few seconds and then refuses it, and DNS cannot
// leak, as every name is asked of the tunnel's resolver. The route to the
// server stays too, so a connection made again finds its way. The state is
// Blocked, the connection is retried with a growing pause, and the first
// success is Connected again. Only the user's Disconnect (or turning the
// setting off, which the next retry sees) lets the traffic out.
//
// What stays outside by the user's own choice stays outside: the local
// network (ExcludeLAN), and the sites, apps and addresses set to go direct,
// which never went through a core.

// ErrTUNRevoked is the error of a TUN layer the system took away (Android:
// another VPN started, the user switched CoreShift off in the settings).
// The kill switch does not hold that: the system, not a failure, ended the
// VPN, and building it anew would fight the user's choice.
var ErrTUNRevoked = errors.New("the system took the VPN away")

// errBlockedNoNetwork is what an attempt made while Blocked returns when the
// device has no network: the retries go on, the block stays.
var errBlockedNoNetwork = errors.New("blocked: no network to connect through")

// Pauses between the retries of a blocked connection: the first is quick,
// as most failures are a core that hiccuped; the last is how often a server
// that stays down is asked.
const (
	blockedRetryMin = 5 * time.Second
	blockedRetryMax = 30 * time.Second
)

func (s *Service) isBlocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blocked
}

// holdBlocked answers a failed connection, called with s.op held: when the
// kill switch is on and a TUN layer was fully up, the connection is held
// Blocked instead of torn down, and true is returned. False leaves the
// teardown to the caller. Called again by a retry that failed, it keeps the
// hold without announcing it twice.
func (s *Service) holdBlocked(cause error) bool {
	s.mu.Lock()
	hold := s.opts.KillSwitch && s.connOpts.TUN && s.hasTunLookups && s.hasLast && !errors.Is(cause, ErrTUNRevoked)
	if !hold {
		s.mu.Unlock()
		return false
	}
	already := s.blocked
	n, tun := s.lastNode, s.tun
	s.blocked = true
	var ctx context.Context
	if s.blockCancel == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		s.blockCancel = cancel
	}
	s.mu.Unlock()
	// A layer that died is dropped, the DNS redirect and the server's route
	// are not: the retry builds the layer again under them.
	if tun != nil {
		select {
		case <-tun.Exited():
			tun.Stop()
			s.mu.Lock()
			if s.tun == tun {
				s.tun = nil
			}
			s.mu.Unlock()
			s.hub.publish(Event{Kind: "tun", Reason: "down"})
		default:
		}
	}
	s.sup.Disconnect() // the cores of a failed connection are of no use
	if !already {
		st := Status{State: Blocked, Node: n.Name, Protocol: string(n.Protocol), TUN: true, Error: cause.Error(), Since: time.Now()}
		if m, ok := msg.Of(cause); ok {
			st.ErrorCode, st.ErrorArgs = m.Code, m.Args
		}
		s.setStatus(st)
		s.hub.publish(Event{Kind: "killswitch", Reason: "blocked"}.withLine(msg.New("killswitch.blocked")))
	}
	if ctx != nil {
		go s.retryBlocked(ctx)
	}
	return true
}

// leaveBlocked ends the hold when the connection is made again.
func (s *Service) leaveBlocked() {
	s.mu.Lock()
	s.blocked = false
	if s.blockCancel != nil {
		s.blockCancel()
		s.blockCancel = nil
	}
	s.mu.Unlock()
}

// retryBlocked connects the last server again, with a growing pause, until
// it works or ctx ends (a disconnect; a connection made by the user).
func (s *Service) retryBlocked(ctx context.Context) {
	delay := cmp.Or(s.cfg.blockedRetry, blockedRetryMin)
	for {
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		delay = min(delay*2, max(blockedRetryMax, delay))
		opCtx, end := s.beginOp(ctx)
		s.mu.Lock()
		n, held := s.lastNode, s.blocked && ctx.Err() == nil
		s.mu.Unlock()
		if !held {
			end()
			return
		}
		err := s.connectNode(opCtx, n, true)
		end()
		if err == nil {
			return // connected, or another connection took over
		}
	}
}

// stopConn takes the running connection down before another is made: all of
// it, or, while held by the kill switch, the cores and the TUN layer only,
// leaving the DNS redirect and the server's route in place.
func (s *Service) stopConn() {
	if !s.isBlocked() {
		s.stopLocked()
		return
	}
	s.mu.Lock()
	s.gen++ // turns pending teardowns for this connection into no-ops
	s.tunDNS = netip.AddrPort{}
	s.dnsDirect.disarm()
	s.mu.Unlock()
	s.netDown.Store(false)
	if s.stopPing != nil {
		s.stopPing()
		s.stopPing = nil
	}
	s.hub.clearTraffic()
	if s.tun != nil {
		s.tun.Stop()
		s.tun = nil
		s.hub.publish(Event{Kind: "tun", Reason: "down"})
	}
	s.sup.Disconnect()
	s.logs.flushAll()
}
