package service

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/supervisor"
)

// network makes pings answer from the addresses in up only, as seen from
// the physical interface, which is there unless noBind.
func network(noBind bool, up ...string) func(*Config, *store.Settings) {
	return func(c *Config, _ *store.Settings) {
		if c == nil {
			return
		}
		c.physical = func() (ping.Bind, error) {
			if noBind {
				return ping.Bind{}, errors.New("no physical interface")
			}
			return ping.Bind{}, nil
		}
		answers := func(a string) (time.Duration, error) {
			if slices.Contains(up, a) {
				return 20 * time.Millisecond, nil
			}
			return 0, errors.New("i/o timeout")
		}
		c.tcpPing = func(_ context.Context, ap netip.AddrPort, _ ping.Bind) (time.Duration, error) {
			return answers(ap.String())
		}
		c.icmpPing = func(_ context.Context, ip netip.Addr, _ ping.Bind) (time.Duration, error) {
			return answers(ip.String())
		}
	}
}

func switchServers(on bool) func(*Config, *store.Settings) {
	return func(_ *Config, s *store.Settings) {
		if s != nil {
			s.Cores.SwitchServer = on
		}
	}
}

func TestReachTellsServerFromNetwork(t *testing.T) {
	n := trojan("Польша", "203.0.113.13")
	ip := netip.MustParseAddr("203.0.113.13")
	hy := node.Node{Name: "hy", Protocol: node.Hysteria2, Server: "203.0.113.13", Port: 443}
	for name, c := range map[string]struct {
		cfg     func(*Config, *store.Settings)
		outside bool
		n       node.Node
		want    Reach
	}{
		"the server is down, the internet is not": {cfg: network(false, "1.1.1.1:443"), n: n, want: ReachServerDown},
		"only Yandex answers":                     {cfg: network(false, "77.88.8.8:443"), n: n, want: ReachServerDown},
		"nothing answers":                         {cfg: network(false), n: n, want: ReachOffline},
		"the server answers, the tunnel does not": {cfg: network(false, "203.0.113.13:443", "1.1.1.1:443"), n: n, want: ReachServerUp},
		// Through the tunnel, which is down, the well-known hosts prove nothing.
		"no way around the tunnel": {cfg: network(true), n: n, want: ReachUnknown},
		// Android keeps the app outside its VPN: the default route will do.
		"android":           {cfg: network(true, "8.8.8.8:443"), outside: true, n: n, want: ReachServerDown},
		"a server over UDP": {cfg: network(false, "203.0.113.13", "1.1.1.1:443"), n: hy, want: ReachServerUp},
	} {
		cfg := Config{AppOutsideVPN: c.outside}
		c.cfg(&cfg, nil)
		s := &Service{cfg: cfg}
		if got, detail := s.checkReach(context.Background(), c.n, ip); got != c.want {
			t.Errorf("%s: %s (%s), want %s", name, got, detail, c.want)
		}
	}
}

// waitEvent returns the first event of kind, or fails.
func waitEvent(t *testing.T, events <-chan Event, kind string) Event {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == kind {
				return e
			}
		case <-deadline:
			t.Fatalf("no %q event", kind)
		}
	}
}

// A server picked by hand that does not answer, while the internet does:
// the next server of the subscription that answers is connected, and the
// UI is told which and why.
func TestSwitchesFromADeadServerOfASubscription(t *testing.T) {
	h, st, _ := switchHarness(t, plainLinks, "proxy", network(false, "1.1.1.1:443", "203.0.113.3:443"))
	events, unsubscribe := h.svc.Subscribe(false)
	defer unsubscribe()
	serverNotAnswering(h)
	e := waitEvent(t, events, "server")
	if e.Reason != string(ReachServerDown) || e.From != "proxy" {
		t.Errorf("server event = %+v", e)
	}
	// proxy-2 does not answer either: it is passed over.
	waitNode(t, h, "proxy-3")
	if sel, _, _ := st.Selected(); sel.Name != "proxy-3" {
		t.Errorf("selection = %q", sel.Name)
	}
	if p := h.svc.Status().Problem; p != "" {
		t.Errorf("the new server inherited the problem %q", p)
	}
}

func TestStaysWhenSwitchingIsOff(t *testing.T) {
	h, _, _ := switchHarness(t, plainLinks, "proxy", network(false, "1.1.1.1:443", "203.0.113.3:443"), switchServers(false))
	events, unsubscribe := h.svc.Subscribe(false)
	defer unsubscribe()
	serverNotAnswering(h)
	waitEvent(t, events, "server")
	time.Sleep(300 * time.Millisecond)
	st := h.svc.Status()
	if st.State != Connected || st.Node != "proxy" || st.Problem != string(ReachServerDown) {
		t.Errorf("status = %+v: must stay, saying the server is down", st)
	}
	// It answers again: the problem goes, and the UI hears it.
	h.svc.onSupervisorEvent(supervisorHealthOK())
	if e := waitEvent(t, events, "server"); e.Reason != "ok" {
		t.Errorf("event = %+v", e)
	}
	if p := h.svc.Status().Problem; p != "" {
		t.Errorf("problem after recovery: %q", p)
	}
}

// Without internet no other server would answer: nothing moves, not even
// in a panel's automatic selection.
func TestNoSwitchWhenOffline(t *testing.T) {
	for _, content := range []string{plainLinks, panelJSON} {
		h, _, _ := switchHarness(t, content, "proxy", network(false))
		events, unsubscribe := h.svc.Subscribe(false)
		serverNotAnswering(h)
		if e := waitEvent(t, events, "server"); e.Reason != string(ReachOffline) {
			t.Errorf("event = %+v", e)
		}
		time.Sleep(300 * time.Millisecond)
		if st := h.svc.Status(); st.Node != "proxy" || st.Problem != string(ReachOffline) {
			t.Errorf("status = %+v", st)
		}
		unsubscribe()
	}
}

// A server that answers a handshake is blocked or misconfigured, not down:
// the user's pick stays; the panel's group still moves on.
func TestNoSwitchFromAServerThatAnswers(t *testing.T) {
	h, _, _ := switchHarness(t, plainLinks, "proxy", network(false, "1.1.1.1:443", "203.0.113.1:443", "203.0.113.2:443"))
	events, unsubscribe := h.svc.Subscribe(false)
	defer unsubscribe()
	serverNotAnswering(h)
	if e := waitEvent(t, events, "server"); e.Reason != string(ReachServerUp) {
		t.Errorf("event = %+v", e)
	}
	time.Sleep(300 * time.Millisecond)
	if st := h.svc.Status(); st.Node != "proxy" {
		t.Errorf("status = %+v", st)
	}
}

func supervisorHealthOK() supervisor.Event {
	return supervisor.Event{Kind: supervisor.EventHealth, Latency: time.Millisecond}
}
