package service

import (
	"context"
	"sync"

	"coreshift/engine/internal/node"
	"coreshift/engine/internal/store"
)

// Moving to the next server. A core can be swapped for another when it
// fails, but when the server itself is down, or blocked, every core fails
// alike: the supervisor reports it (EventNoBetter) and leaves the connection
// as it is. When the subscription says its servers are for automatic
// selection (an Xray balancer in a JSON subscription, Subscription.Auto) and
// the connected server is one of them, the service connects the next of them,
// in the order the subscription lists them, until one answers. Servers that
// did not answer in this round are not tried again; the round ends when a
// server answers or the user connects.

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
// the list once: not from itself, not tried, and accepted by ok.
func pickNext(nodes []node.Node, from string, tried map[string]bool, ok func(node.Node) bool) (node.Node, bool) {
	start := -1
	for i := range nodes {
		if nodes[i].Fingerprint() == from {
			start = i
			break
		}
	}
	for step := 1; step <= len(nodes); step++ {
		n := nodes[(start+step+len(nodes))%len(nodes)]
		if fp := n.Fingerprint(); fp != from && !tried[fp] && ok(n) {
			return n, true
		}
	}
	return node.Node{}, false
}

// subscriptionOf finds the subscription that lists the node with the
// fingerprint; the selected one first, as the same server may be in several.
func subscriptionOf(subs []store.Subscription, selected, fingerprint string) (store.Subscription, bool) {
	has := func(sub store.Subscription) bool {
		for i := range sub.Nodes {
			if sub.Nodes[i].Fingerprint() == fingerprint {
				return true
			}
		}
		return false
	}
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

// failoverSoon switches servers in the background, if the connection is up.
// Whether its subscription asks for it is seen when it is about to switch.
func (s *Service) failoverSoon() {
	if s.cfg.Store == nil {
		return
	}
	s.mu.Lock()
	gen, connected := s.gen, s.status.State == Connected
	s.mu.Unlock()
	if !connected || !s.fo.begin() {
		return
	}
	go func() {
		defer s.fo.end()
		s.switchServer(gen)
	}()
}

// switchServer connects the next server that works, for the connection gen
// that stopped answering. It gives up when none is left; the connection then
// stays on the last one tried.
func (s *Service) switchServer(gen int) {
	s.op.Lock()
	defer s.op.Unlock()
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
	if !group[from.Fingerprint()] {
		return // the panel set up no automatic selection, or this server is not in it
	}
	cur := from
	for {
		s.fo.markTried(cur.Fingerprint())
		next, ok := pickNext(sub.Nodes, cur.Fingerprint(), s.fo.triedSet(), func(n node.Node) bool {
			return group[n.Fingerprint()] && len(s.Compatible(&n)) > 0
		})
		if !ok {
			s.hub.publish(Event{Kind: "failover", From: from.Name, Error: "no other server of the automatic selection answers"})
			return
		}
		if _, err := st.Select(sub.ID, next.Fingerprint(), next.Name); err != nil {
			return
		}
		s.hub.publish(Event{Kind: "failover", From: cur.Name, Line: next.Name})
		if err := s.connectOp(context.Background(), next); err == nil {
			return
		}
		// It would not even start: on to the one after it.
		cur = next
	}
}
