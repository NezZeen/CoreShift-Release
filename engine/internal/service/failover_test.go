package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"coreshift/engine/internal/node"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/supervisor"
)

func trojan(name, host string) node.Node {
	return node.Node{Name: name, Protocol: node.Trojan, Server: host, Port: 443, Password: "pw"}
}

func TestPickNext(t *testing.T) {
	nodes := []node.Node{trojan("a", "a.example"), trojan("b", "b.example"), trojan("c", "c.example"), trojan("d", "d.example")}
	fp := func(i int) string { return nodes[i].Fingerprint() }
	any := func(node.Node) bool { return true }

	for name, tc := range map[string]struct {
		from  int
		tried map[string]bool
		ok    func(node.Node) bool
		want  string
	}{
		"the next in the list":         {from: 0, ok: any, want: "b"},
		"wraps round to the first":     {from: 3, ok: any, want: "a"},
		"skips the ones tried":         {from: 0, tried: map[string]bool{fp(1): true}, ok: any, want: "c"},
		"skips the ones not accepted":  {from: 0, ok: func(n node.Node) bool { return n.Name != "b" }, want: "c"},
		"comes back to an earlier one": {from: 2, tried: map[string]bool{fp(3): true}, ok: any, want: "a"},
		"nothing is left":              {from: 0, tried: map[string]bool{fp(1): true, fp(2): true, fp(3): true}, ok: any, want: ""},
	} {
		got, ok := pickNext(nodes, fp(tc.from), tc.tried, tc.ok)
		if (tc.want == "") == ok || got.Name != tc.want {
			t.Errorf("%s: got %q (%v), want %q", name, got.Name, ok, tc.want)
		}
	}
	// The server is not in the list any more: begin at the first.
	if got, ok := pickNext(nodes, "gone", nil, any); !ok || got.Name != "a" {
		t.Errorf("a server missing from the list: got %q, want the first", got.Name)
	}
}

// switchHarness connects the first of three servers of a subscription.
func switchHarness(t *testing.T, autoSwitch bool) (*harness, *store.Store, store.Subscription) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set := st.Settings()
	set.AutoSwitch = autoSwitch
	// The health checks must not interfere: the test sends the events itself.
	set.Cores.HealthURL, set.Cores.HealthIntervalS, set.Cores.HealthFailures = "http://health.test/generate_204", 3600, 2
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) { c.Store = st })
	sub, err := st.Add(context.Background(), store.AddRequest{Name: "s", Content: "trojan://pw@203.0.113.1:443#One\ntrojan://pw@203.0.113.2:443#Two\ntrojan://pw@203.0.113.3:443#Three"})
	if err != nil {
		t.Fatal(err)
	}
	n := sub.Nodes[0]
	if _, err := st.Select(sub.ID, n.Fingerprint(), n.Name); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ConnectSelected(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, Connected, 10*time.Second)
	return h, st, sub
}

// serverNotAnswering is what the supervisor reports when the connection
// fails its checks and no core does better.
func serverNotAnswering(h *harness) {
	// A real report comes many seconds after a switch; here it must not
	// overtake the end of the one before.
	for i := 0; i < 500; i++ {
		h.svc.fo.mu.Lock()
		busy := h.svc.fo.running
		h.svc.fo.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.svc.onSupervisorEvent(supervisor.Event{Kind: supervisor.EventNoBetter, Err: errors.New("health check failed")})
}

func waitNode(t *testing.T, h *harness, name string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if st := h.svc.Status(); st.State == Connected && st.Node == name {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never connected to %q; status %+v", name, h.svc.Status())
}

func TestFailoverGoesDownTheList(t *testing.T) {
	h, st, _ := switchHarness(t, true)

	serverNotAnswering(h)
	waitNode(t, h, "Two")
	if sel, _, _ := st.Selected(); sel.Name != "Two" {
		t.Errorf("selection = %q, want it to follow the connection", sel.Name)
	}

	serverNotAnswering(h)
	waitNode(t, h, "Three")
}

// When every other server of the round was tried, the connection stays and
// the UI is told. (The test's core answers its checks, which would end a
// round at once, so the round is set up by hand.)
func TestFailoverStopsWhenNoServerIsLeft(t *testing.T) {
	h, _, sub := switchHarness(t, true)
	h.svc.fo.markTried(sub.Nodes[1].Fingerprint())
	h.svc.fo.markTried(sub.Nodes[2].Fingerprint())

	events, unsubscribe := h.svc.Subscribe(false)
	defer unsubscribe()
	serverNotAnswering(h)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == "failover" {
				if e.Error == "" {
					t.Fatalf("switched to %q although every other server was tried", e.Line)
				}
				if st := h.svc.Status(); st.State != Connected || st.Node != "One" {
					t.Errorf("status after giving up = %+v, want to stay on One", st)
				}
				return
			}
		case <-deadline:
			t.Fatal("no failover event said that no server answers")
		}
	}
}

func TestFailoverAnnouncesTheSwitch(t *testing.T) {
	h, _, _ := switchHarness(t, true)
	events, unsubscribe := h.svc.Subscribe(false)
	defer unsubscribe()
	serverNotAnswering(h)
	deadline := time.After(20 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == "failover" {
				if e.From != "One" || e.Line != "Two" || e.Error != "" {
					t.Errorf("event = %+v, want One → Two", e)
				}
				return
			}
		case <-deadline:
			t.Fatal("no failover event")
		}
	}
}

func TestFailoverRoundRestartsWhenAServerAnswers(t *testing.T) {
	h, _, _ := switchHarness(t, true)
	serverNotAnswering(h)
	waitNode(t, h, "Two")
	// Two answers its check: the round is over, and when it fails later
	// the first server gets another chance.
	h.svc.onSupervisorEvent(supervisor.Event{Kind: supervisor.EventHealth, Latency: time.Millisecond})
	serverNotAnswering(h)
	waitNode(t, h, "Three")
	h.svc.onSupervisorEvent(supervisor.Event{Kind: supervisor.EventHealth, Latency: time.Millisecond})
	serverNotAnswering(h)
	waitNode(t, h, "One")
}

func TestFailoverIsOffByDefault(t *testing.T) {
	h, _, _ := switchHarness(t, false)
	serverNotAnswering(h)
	time.Sleep(300 * time.Millisecond)
	if st := h.svc.Status(); st.State != Connected || st.Node != "One" {
		t.Errorf("status = %+v: the setting is off, the connection must stay", st)
	}
}

func TestFailoverLeavesAPastedLinkAlone(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set := st.Settings()
	set.AutoSwitch = true
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) { c.Store = st })
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	serverNotAnswering(h)
	time.Sleep(300 * time.Millisecond)
	if got := h.svc.Status(); got.State != Connected || got.Node != "Trojan" {
		t.Errorf("status = %+v: a one-off link has no list to move down", got)
	}
}
