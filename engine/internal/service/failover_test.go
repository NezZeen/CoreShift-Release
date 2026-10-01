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

// panelJSON is an Xray JSON subscription as Remnawave serves it: a balancer
// over the outbounds tagged "proxy…", and one more server, "fixed", outside it.
const panelJSON = `{
  "routing": {"balancers": [{"tag": "Super_Balancer", "selector": ["proxy"], "strategy": {"type": "leastLoad"}, "fallbackTag": "direct"}]},
  "outbounds": [
    {"tag": "proxy",   "protocol": "trojan", "settings": {"servers": [{"address": "203.0.113.1", "port": 443, "password": "pw"}]}, "streamSettings": {"security": "tls"}},
    {"tag": "proxy-2", "protocol": "trojan", "settings": {"servers": [{"address": "203.0.113.2", "port": 443, "password": "pw"}]}, "streamSettings": {"security": "tls"}},
    {"tag": "proxy-3", "protocol": "trojan", "settings": {"servers": [{"address": "203.0.113.3", "port": 443, "password": "pw"}]}, "streamSettings": {"security": "tls"}},
    {"tag": "fixed",   "protocol": "trojan", "settings": {"servers": [{"address": "203.0.113.4", "port": 443, "password": "pw"}]}, "streamSettings": {"security": "tls"}},
    {"tag": "direct", "protocol": "freedom"}
  ]
}`

// plainLinks is a subscription of links: no automatic selection.
const plainLinks = "trojan://pw@203.0.113.1:443#proxy\ntrojan://pw@203.0.113.2:443#proxy-2\ntrojan://pw@203.0.113.3:443#proxy-3"

// switchHarness adds the subscription and connects its server named first.
func switchHarness(t *testing.T, content, first string) (*harness, *store.Store, store.Subscription) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set := st.Settings()
	// The health checks must not interfere: the test sends the events itself.
	set.Cores.HealthURL, set.Cores.HealthIntervalS, set.Cores.HealthFailures = "http://health.test/generate_204", 3600, 2
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) { c.Store = st })
	sub, err := st.Add(context.Background(), store.AddRequest{Name: "s", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range sub.Nodes {
		if n.Name == first {
			if _, err := st.Select(sub.ID, n.Fingerprint(), n.Name); err != nil {
				t.Fatal(err)
			}
		}
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

func TestFailoverGoesDownThePanelsGroup(t *testing.T) {
	h, st, sub := switchHarness(t, panelJSON, "proxy")
	if len(sub.Auto) != 3 {
		t.Fatalf("the group has %d servers, want the three proxy-*: %v", len(sub.Auto), sub.Auto)
	}

	serverNotAnswering(h)
	waitNode(t, h, "proxy-2")
	if sel, _, _ := st.Selected(); sel.Name != "proxy-2" {
		t.Errorf("selection = %q, want it to follow the connection", sel.Name)
	}
	serverNotAnswering(h)
	waitNode(t, h, "proxy-3")
	// Past the end it comes round to the first, and never to the server
	// the panel left out of the group.
	h.svc.onSupervisorEvent(supervisor.Event{Kind: supervisor.EventHealth, Latency: time.Millisecond})
	serverNotAnswering(h)
	waitNode(t, h, "proxy")
}

// When every other server of the round was tried, the connection stays and
// the UI is told. (The test's core answers its checks, which would end a
// round at once, so the round is set up by hand.)
func TestFailoverStopsWhenNoServerIsLeft(t *testing.T) {
	h, _, sub := switchHarness(t, panelJSON, "proxy")
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
				if st := h.svc.Status(); st.State != Connected || st.Node != "proxy" {
					t.Errorf("status after giving up = %+v, want to stay on proxy", st)
				}
				return
			}
		case <-deadline:
			t.Fatal("no failover event said that no server answers")
		}
	}
}

func TestFailoverAnnouncesTheSwitch(t *testing.T) {
	h, _, _ := switchHarness(t, panelJSON, "proxy")
	events, unsubscribe := h.svc.Subscribe(false)
	defer unsubscribe()
	serverNotAnswering(h)
	deadline := time.After(20 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == "failover" {
				if e.From != "proxy" || e.Line != "proxy-2" || e.Error != "" {
					t.Errorf("event = %+v, want proxy → proxy-2", e)
				}
				return
			}
		case <-deadline:
			t.Fatal("no failover event")
		}
	}
}

func TestFailoverRoundRestartsWhenAServerAnswers(t *testing.T) {
	h, _, _ := switchHarness(t, panelJSON, "proxy")
	serverNotAnswering(h)
	waitNode(t, h, "proxy-2")
	// proxy-2 answers its check: the round is over, and when it fails later
	// the first server gets another chance.
	h.svc.onSupervisorEvent(supervisor.Event{Kind: supervisor.EventHealth, Latency: time.Millisecond})
	serverNotAnswering(h)
	waitNode(t, h, "proxy-3")
	h.svc.onSupervisorEvent(supervisor.Event{Kind: supervisor.EventHealth, Latency: time.Millisecond})
	serverNotAnswering(h)
	waitNode(t, h, "proxy")
}

// Without a group from the panel nothing moves by itself.
func TestNoFailoverWithoutAnAutomaticSelection(t *testing.T) {
	h, _, sub := switchHarness(t, plainLinks, "proxy")
	if len(sub.Auto) != 0 {
		t.Fatalf("a list of links has a group: %v", sub.Auto)
	}
	serverNotAnswering(h)
	time.Sleep(300 * time.Millisecond)
	if st := h.svc.Status(); st.State != Connected || st.Node != "proxy" {
		t.Errorf("status = %+v: the connection must stay", st)
	}
}

// A server the panel kept out of the group is the user's own pick.
func TestNoFailoverFromAServerOutsideTheGroup(t *testing.T) {
	h, _, _ := switchHarness(t, panelJSON, "fixed")
	serverNotAnswering(h)
	time.Sleep(300 * time.Millisecond)
	if st := h.svc.Status(); st.State != Connected || st.Node != "fixed" {
		t.Errorf("status = %+v: the connection must stay", st)
	}
}

func TestNoFailoverForAPastedLink(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
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
