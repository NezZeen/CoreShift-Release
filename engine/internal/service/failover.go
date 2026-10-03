package service

import (
	"context"
	"net/netip"
	"slices"
	"sync"

	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/store"
)

// Moving to the next server. A core can be swapped for another when it
// fails, but when the server itself is down, or blocked, every core fails
// alike: the supervisor reports it (EventNoBetter) and leaves the connection
// as it is, and the service looks whether the server or the network is at
// fault (checkReach). Unless the network is down, it then connects the next
// server, in the order the subscription lists them, until one answers:
//   - when the subscription says its servers are for automatic selection (an
//     Xray balancer in a JSON subscription, Subscription.Auto) and the
//     connected server is one of them, among those;
//   - otherwise, when the user allows it (store.CoreSettings.SwitchServer)
//     and the server itself does not answer, among the subscription's
//     servers that answer a handshake from here.
// Servers that did not answer in this round are not tried again; the round
// ends when a server answers or the user connects.

// failover is the state of one round.
type failover struct {
	mu      sync.Mutex
	running bool
	tried   map[string]bool // fingerprints of the servers that did not answer
}

// begin starts a switch unless one is under way.
func (f *failover) begin() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running {
		return false
	}
	f.running = true
	return true
}

func (f *failover) end() {
	f.mu.Lock()
	f.running = false
	f.mu.Unlock()
}

func (f *failover) markTried(fingerprint string) {
	f.mu.Lock()
	if f.tried == nil {
		f.tried = map[string]bool{}
	}
	f.tried[fingerprint] = true
	f.mu.Unlock()
}

func (f *failover) triedSet() map[string]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]bool, len(f.tried))
	for k := range f.tried {
		out[k] = true
	}
	return out
}

// reset ends the round.
func (f *failover) reset() {
	f.mu.Lock()
	if len(f.tried) > 0 {
		f.tried = nil
	}
	f.mu.Unlock()
}

// pickNext returns the first server after from that is usable, going round
// the list once: not from itself, not tried, and accepted by ok. fps are
// the nodes' fingerprints.
func pickNext(nodes []node.Node, fps []string, from string, tried map[string]bool, ok func(node.Node) bool) (node.Node, bool) {
	start := slices.Index(fps, from)
	for step := 1; step <= len(nodes); step++ {
		i := (start + step + len(nodes)) % len(nodes)
		if fp := fps[i]; fp != from && !tried[fp] && ok(nodes[i]) {
			return nodes[i], true
		}
	}
	return node.Node{}, false
}

// subscriptionOf finds the subscription that lists the node with the
// fingerprint; the selected one first, as the same server may be in several.
func subscriptionOf(subs []store.Subscription, selected, fingerprint string) (store.Subscription, bool) {
	has := func(sub store.Subscription) bool { return slices.Contains(sub.Fingerprints(), fingerprint) }
	for _, sub := range subs {
		if sub.ID == selected && has(sub) {
			return sub, true
		}
	}
	for _, sub := range subs {
		if has(sub) {
			return sub, true
		}
	}
	return store.Subscription{}, false
}

// failoverSoon switches servers in the background, if the connection is up,
// after checkReach found r. Whether its subscription asks for it, or the
// user allows it, is seen when it is about to switch.
func (s *Service) failoverSoon(r Reach) {
	if s.cfg.Store == nil || r == ReachOffline {
		return // without internet another server would not answer either
	}
	s.mu.Lock()
	gen, connected := s.gen, s.status.State == Connected
	s.mu.Unlock()
	if !connected || !s.fo.begin() {
		return
	}
	go func() {
		defer s.fo.end()
		s.switchServer(gen, r)
	}()
}

// switchServer connects the next server that works, for the connection gen
// that stopped answering. It gives up when none is left; the connection then
// stays on the last one tried.
func (s *Service) switchServer(gen int, r Reach) {
	ctx, end := s.beginOp(context.Background())
	defer end()
	st := s.cfg.Store
	s.mu.Lock()
	current, from := gen == s.gen && s.status.State == Connected, s.lastNode
	s.mu.Unlock()
	if !current {
		return // the user disconnected or connected another meanwhile
	}
	selected, _, _ := st.Selected()
	sub, ok := subscriptionOf(st.Subscriptions(), selected.Subscription, from.Fingerprint())
	if !ok {
		return // a node from a pasted link: there is no list to go through
	}
	group := map[string]bool{}
	for _, fp := range sub.Auto {
		group[fp] = true
	}
	// answers: the panel's group is taken as it is; any other server of
	// the subscription must answer a handshake, so the switch does not
	// land on one as dead as this one.
	answers := func(node.Node) bool { return true }
	if !group[from.Fingerprint()] {
		// The panel set up no automatic selection, or this server is not
		// in it: the user's own pick, left only when the server itself is
		// down and the user allows moving on.
		if r != ReachServerDown || !s.Options().SwitchServer {
			return
		}
		clear(group)
		for _, fp := range sub.Fingerprints() {
			group[fp] = true
		}
		answers = func(n node.Node) bool { return s.serverAnswers(ctx, n) }
	}
	cur := from
	for {
		if ctx.Err() != nil {
			return // the user disconnected: no more servers to try
		}
		s.fo.markTried(cur.Fingerprint())
		next, ok := pickNext(sub.Nodes, sub.Fingerprints(), cur.Fingerprint(), s.fo.triedSet(), func(n node.Node) bool {
			return group[n.Fingerprint()] && len(s.Compatible(&n)) > 0 && answers(n)
		})
		if !ok {
			s.hub.publish(Event{Kind: "failover", From: from.Name, Error: "no other server of the subscription answers"})
			return
		}
		if _, err := st.Select(sub.ID, next.Fingerprint(), next.Name); err != nil {
			return
		}
		s.hub.publish(Event{Kind: "failover", From: cur.Name, Line: next.Name})
		if err := s.connectOp(ctx, next); err == nil || ctx.Err() != nil {
			return
		}
		// It would not even start: on to the one after it.
		cur = next
	}
}

// serverAnswers reports whether n's server takes a TCP handshake from here,
// around the tunnel. A server over UDP has no port to try and counts as
// answering: connecting it tells.
func (s *Service) serverAnswers(ctx context.Context, n node.Node) bool {
	if overUDP(&n) {
		return true
	}
	ip, err := s.serverAddr(ctx, n.Server)
	if err != nil {
		return false
	}
	bind, err := s.cfg.physical()
	if err != nil {
		bind = ping.Bind{}
	}
	_, err = s.cfg.tcpPing(ctx, netip.AddrPortFrom(ip, n.Port), bindFor(bind, ip))
	return err == nil
}
