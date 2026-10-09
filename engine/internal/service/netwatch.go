package service

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/tunlayer"
)

// Losing the network. A computer that drops its Wi-Fi or its cable, or a
// phone in airplane mode, has no default route: nothing outside it can be
// reached, through the tunnel or around it. Every check of every core then
// fails, and the service used to take that for the core's fault (it swapped
// through every core), then for the server's ("the server does not answer"),
// and a connection made meanwhile failed at once on the server's name. Now
// "no network" is a state of its own, NoNetwork, and the service waits:
//
//   - Connecting without a network does not fail. Nothing is started; the
//     state is NoNetwork with Status.Waiting, and the connection is made as
//     soon as the network is there (awaitNetwork). Disconnect cancels it, as
//     it cancels any connecting. A network missing only for the moment a
//     reconnect takes the tunnel down does not count (networkGone).
//   - A connection that loses the network is held as it is (watchNet): the
//     cores keep running, the supervisor counts no failed checks (its
//     Config.Offline), the server is not diagnosed and no other server is
//     tried. When the network returns the state is Connected again; if
//     nothing has got through the tunnel netBackGrace later, the connection
//     is made anew.
//
// While a connection is held its TUN layer and the DNS redirect stay. There
// is nothing to leak without a network, but the moment it returns apps go
// on at once, sooner than any reconnect could: their first lookups and
// connections go into the tunnel rather than out in the open. Nor does the
// redirect keep the returning network from working: the tunnel is not dead.
// sing-box keeps answering DNS (with fake addresses, or through the core),
// the server's address stays routed around the tunnel, and the cores dial
// the server again by themselves. Where that is not enough - a network
// with other resolvers is watchNetwork's, which reconnects; one where the
// cores cannot reach the server (a login page first) fails the checks past
// netBackGrace - the connection is made anew, and from then on fails as any
// connection does: a connection that cannot start is taken down and the
// system gets its DNS back.
//
// What "no network" is: no default route outside the tunnel on the desktop
// (ping.HasDefaultRoute), what ConnectivityManager reports on Android
// (Config.NetworkUp). Neither is trusted against evidence: when a check
// passes or the server's name resolves while it says "no network", the
// source is taken to be blind (netBlind) until it sees a network again, so
// a computer whose routes look odd never waits for nothing.

const (
	// netPollInterval is how often a connection looks whether the network
	// is there; sing-box's own complaint (netHint) makes it look at once.
	netPollInterval = 2 * time.Second
	// netConfirm looks in a row without a network hold a connection: an
	// adapter that flaps for a moment does not.
	netConfirm = 2
	// netBackGrace is how long a connection may take to carry traffic
	// again once the network returned, before it is made anew.
	netBackGrace = 30 * time.Second
	// netEvidenceInterval is how often a connection waiting for the network
	// tries the server anyway (serverAnswersNow).
	netEvidenceInterval = 20 * time.Second
	// netSettleTime is how long a connection that finds no network looks
	// again before it waits for one (networkGone), every netSettleLook.
	netSettleTime = 1500 * time.Millisecond
	netSettleLook = 100 * time.Millisecond
)

// hasNetwork is the desktop's NetworkUp: a default route outside the
// tunnel. When the route table cannot be read it says yes: waiting for a
// network that is there would be worse than not waiting.
func hasNetwork() bool {
	ok, err := ping.HasDefaultRoute(tunlayer.DefaultInterface)
	return ok || err != nil
}

// noNetwork reports that the device has no network, unless the source was
// caught wrong (see netBlind).
func (s *Service) noNetwork() bool {
	if s.cfg.netUp() {
		s.netBlind.Store(false)
		return false
	}
	return !s.netBlind.Load()
}

// networkGone reports that the device has no network, and still has none
// cfg.netSettle later. A reconnect takes the tunnel down and puts it up
// again, and for that moment the device itself can look offline: Android's
// ConnectivityManager reports no default network while the VPN is torn
// down and before the phone's own network is the default again, and a
// desktop's route table can be in between too. Taken at its word, the
// journal said "no network: connecting once it is there" and "the network
// is back" in the same second. A network that returns meanwhile, which
// Android tells at once (NetworkChanged), connects without a word; a real
// outage waits only cfg.netSettle longer. False when ctx ends first.
func (s *Service) networkGone(ctx context.Context) bool {
	if !s.noNetwork() {
		return false
	}
	settled := time.NewTimer(s.cfg.netSettle)
	defer settled.Stop()
	look := time.NewTicker(min(netSettleLook, s.cfg.netSettle))
	defer look.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-settled.C:
			return s.noNetwork()
		case <-look.C:
		case <-s.netKick:
		}
		if !s.noNetwork() {
			return false
		}
	}
}

// offline is the supervisor's Config.Offline: a held connection, or no
// network right now.
func (s *Service) offline() bool { return s.netDown.Load() || s.noNetwork() }

// NetworkChanged tells the service that the device's network came or went
// (Android's ConnectivityManager): it looks at once rather than at its next
// look, which with the screen off may be a while.
func (s *Service) NetworkChanged() { s.kickNetwork() }

// netPollIdle is how often the network is looked at while the device is
// idle (SetBackground): every wakeup costs a phone's battery, and Android
// tells of changes anyway (NetworkChanged).
const netPollIdle = 15 * time.Second

// netEvery is how long until the watcher looks next.
func (s *Service) netEvery() time.Duration {
	if s.bg.Load() {
		return max(s.cfg.netPoll, netPollIdle)
	}
	return s.cfg.netPoll
}

// kickNetwork makes the watcher look now.
func (s *Service) kickNetwork() {
	select {
	case s.netKick <- struct{}{}:
	default:
	}
}

// netHint recognises what the TUN layer says when the device has no
// default interface: sing-box's network monitor and its dials.
func netHint(line string) bool {
	return strings.Contains(line, "missing default interface") || strings.Contains(line, "no route to internet")
}

// watchNet follows the network for connection gen until ctx ends with it:
// holds the connection when the network goes, lets it go on when the
// network returns, and makes it anew when nothing gets through for
// netBackGrace after that.
func (s *Service) watchNet(ctx context.Context, gen int) {
	t := time.NewTimer(s.netEvery())
	defer t.Stop()
	gone := 0
	var back time.Time // when the network last came back
	// The journal says which network the connection runs over, and when
	// it moves to another.
	names := netNamer{last: s.cfg.netName()}
	if names.last != "" {
		s.hub.publish(Event{Kind: "netinfo", Line: "сеть: " + s.describeNetwork(ctx, names.last)})
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.netKick:
			t.Stop()
		}
		t.Reset(s.netEvery())
		raw := s.cfg.netUp()
		if raw {
			s.netBlind.Store(false)
		} else if s.netSeen.Swap(false) {
			// A check got through while the source says "no network".
			s.netBlind.Store(true)
		}
		up := raw || s.netBlind.Load()
		if raw {
			if line, ok := names.look(ctx, s); ok {
				s.hub.publish(Event{Kind: "netinfo", Reason: "changed", Line: line})
			}
		}
		switch {
		case s.netDown.Load():
			if up && s.resumeNetwork(gen) {
				gone, back = 0, time.Now()
			}
		case !up:
			if gone++; gone >= netConfirm && s.holdForNetwork(gen) {
				back = time.Time{}
			} else if gone < netConfirm {
				// Confirmed soon, even at the idle pace.
				t.Reset(min(s.netEvery(), time.Second))
			}
		default:
			gone = 0
			if back.IsZero() {
				continue
			}
			if s.healthOK.Load() >= back.UnixNano() {
				back = time.Time{} // traffic goes through again
				continue
			}
			if time.Since(back) >= s.cfg.netGrace {
				s.hub.publish(Event{Kind: "network", Reason: "reconnect",
					Line: "сеть вернулась, но связь через сервер не восстановилась: переподключаюсь"})
				s.reconnectGen(gen)
				return
			}
		}
	}
}

// holdForNetwork turns connection gen into NoNetwork; false when it is not
// the connection any more, or another operation runs (the next look tries
// again).
func (s *Service) holdForNetwork(gen int) bool {
	if !s.op.TryLock() {
		return false
	}
	defer s.op.Unlock()
	s.mu.Lock()
	if gen != s.gen || s.status.State != Connected {
		s.mu.Unlock()
		return false
	}
	s.netDown.Store(true)
	s.status.State = NoNetwork
	s.status.Problem = "" // neither the server's fault nor the network's beyond
	st := s.status
	s.mu.Unlock()
	s.hub.publish(Event{Kind: "network", Reason: "lost",
		Line: "сеть пропала: соединение ждёт её, ядра и сервер не меняются"})
	s.publishState(st)
	return true
}

// resumeNetwork lets held connection gen go on: Connected again.
func (s *Service) resumeNetwork(gen int) bool {
	if !s.op.TryLock() {
		return false
	}
	defer s.op.Unlock()
	s.mu.Lock()
	if gen != s.gen || s.status.State != NoNetwork || s.status.Waiting {
		s.mu.Unlock()
		return false
	}
	s.netDown.Store(false)
	s.status.State = Connected
	st := s.status
	s.mu.Unlock()
	s.hub.publish(Event{Kind: "network", Reason: "back", Line: "сеть вернулась"})
	s.publishState(st)
	// Connected again without setStatus: what failed for want of the
	// network is tried now, as after any connect.
	s.retryAfterConnect()
	return true
}

// waitNetwork puts a connection to n that found no network into NoNetwork
// and waits for one in the background (awaitNetwork). Called with s.op held,
// after stopLocked.
func (s *Service) waitNetwork(n node.Node, o Options) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	gen := s.gen
	if s.waitCancel != nil {
		s.waitCancel()
	}
	s.waitCancel = cancel
	s.mu.Unlock()
	s.setStatus(Status{State: NoNetwork, Waiting: true, Node: n.Name, Protocol: string(n.Protocol), TUN: o.TUN, Since: time.Now()})
	s.hub.publish(Event{Kind: "network", Reason: "waiting",
		Line: "сети нет: подключусь, как только она появится"})
	go s.awaitNetwork(ctx, gen, n)
}

// awaitNetwork connects n once the device has a network, for the wait of
// connection gen, until ctx is cancelled (stopLocked: a disconnect, another
// connection).
func (s *Service) awaitNetwork(ctx context.Context, gen int, n node.Node) {
	t := time.NewTimer(s.netEvery())
	defer t.Stop()
	tried := time.Now()
	for !s.netReady(ctx, n, &tried) {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.netKick:
			t.Stop()
		}
		t.Reset(s.netEvery())
	}
	opCtx, end := s.beginOp(context.Background())
	defer end()
	s.mu.Lock()
	current := gen == s.gen && s.status.State == NoNetwork && s.status.Waiting
	s.mu.Unlock()
	if !current || ctx.Err() != nil || disconnected(opCtx) {
		return
	}
	s.hub.publish(Event{Kind: "network", Reason: "back", Line: "сеть появилась"})
	_ = s.connectOp(opCtx, n) // a failure is reported as for any connection
}

// netReady reports whether a wait for the network can end: there is one,
// or the server answers although the source says there is none (then it is
// blind). The server is tried every netEvidence, from tried on.
func (s *Service) netReady(ctx context.Context, n node.Node, tried *time.Time) bool {
	if !s.noNetwork() {
		return true
	}
	if time.Since(*tried) < s.cfg.netEvidence {
		return false
	}
	*tried = time.Now()
	if s.serverAnswersNow(ctx, n) {
		s.netBlind.Store(true)
		return true
	}
	return false
}

// serverAnswersNow tries whether n's server can be reached at all: its name
// resolves, or, given as an address, it takes a TCP handshake.
func (s *Service) serverAnswersNow(ctx context.Context, n node.Node) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ip, err := netip.ParseAddr(n.Server)
	if err != nil {
		_, err = s.cfg.lookup(ctx, n.Server, netip.AddrPort{})
		return err == nil
	}
	if overUDP(&n) {
		return false // nothing to try without the core
	}
	_, err = s.cfg.tcpPing(ctx, netip.AddrPortFrom(ip, n.Port), ping.Bind{})
	return err == nil
}

// publishState tells the UI the state, as setStatus does.
func (s *Service) publishState(st Status) {
	s.hub.publish(Event{Kind: "state", State: st.State, Core: string(st.Core), Error: st.Error})
}
