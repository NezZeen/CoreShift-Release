package service

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"coreshift/engine/internal/store"
)

func waitGone(s *Service, first, grace time.Duration) <-chan bool {
	ch := make(chan bool, 1)
	go func() { ch <- s.WaitAppGone(context.Background(), first, grace) }()
	return ch
}

// «Автозапуск»: the selected server is connected at start only with the
// setting on, and a first attempt that fails (the network is not up yet
// after boot) is tried again.
func TestAutoConnect(t *testing.T) {
	for _, on := range []bool{false, true} {
		st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
		if err != nil {
			t.Fatal(err)
		}
		set := st.Settings()
		set.AutoConnect = on
		set.Cores.HealthURL, set.Cores.HealthIntervalS = "http://health.test/generate_204", 3600
		if _, err := st.SetSettings(set); err != nil {
			t.Fatal(err)
		}
		sub, err := st.Add(context.Background(), store.AddRequest{Name: "s", Content: trojanLink})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Select(sub.ID, sub.Nodes[0].Fingerprint(), sub.Nodes[0].Name); err != nil {
			t.Fatal(err)
		}
		h := newHarness(t, func(c *Config) { c.Store = st })
		// No network at first: the TUN layer cannot start.
		h.tun.startErr, h.tun.failStarts = errors.New("network is unreachable"), 1
		if err := h.svc.AutoConnect(context.Background()); err != nil {
			t.Fatalf("on=%v: %v", on, err)
		}
		want := Idle
		if on {
			want = Connected
		}
		if st := h.svc.Status(); st.State != want {
			t.Errorf("on=%v: %+v, want %s", on, st, want)
		}
		// Each attempt says in the journal why it connects, before it does.
		var got []string
		for len(h.events) > 0 {
			e := <-h.events
			switch {
			case e.Kind == "action":
				got = append(got, e.Source+"|"+e.Line)
			case e.Kind == "state" && e.State == Connecting && len(got) == 0:
				t.Errorf("on=%v: connecting before the action", on)
			}
		}
		wantLines := []string{"|подключить: Trojan (автозапуск)", "|подключить: Trojan (автозапуск), ещё одна попытка"}
		if !on {
			wantLines = nil
		}
		if !slices.Equal(got, wantLines) {
			t.Errorf("on=%v: actions %q, want %q", on, got, wantLines)
		}
	}
}

// A restart of the service in the middle of a connection, and an update
// that interrupted it, say so before connecting again.
func TestResumeSaysWhy(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set := st.Settings()
	set.Cores.HealthURL, set.Cores.HealthIntervalS = "http://health.test/generate_204", 3600
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	sub, err := st.Add(context.Background(), store.AddRequest{Name: "s", Content: trojanLink})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Select(sub.ID, sub.Nodes[0].Fingerprint(), sub.Nodes[0].Name); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) { c.Store = st })
	if err := h.svc.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	for len(h.events) > 0 {
		if e := <-h.events; e.Kind == "action" {
			if e.Source != "служба" || e.Line != "служба перезапустилась посреди соединения, подключаюсь снова: Trojan" {
				t.Errorf("action %+v", e)
			}
			return
		}
	}
	t.Error("no action before connecting")
}

func TestAppGoneWhenNoneEverAttaches(t *testing.T) {
	s := &Service{}
	select {
	case gone := <-waitGone(s, 50*time.Millisecond, time.Hour):
		if !gone {
			t.Fatal("WaitAppGone = false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no app, yet the daemon keeps waiting")
	}
}

func TestAppGoneAfterItsStreamEnds(t *testing.T) {
	s := &Service{}
	detach := s.AttachApp()
	gone := waitGone(s, 50*time.Millisecond, 100*time.Millisecond)
	select {
	case <-gone:
		t.Fatal("gone while the app is attached")
	case <-time.After(300 * time.Millisecond):
	}
	// A restarting app comes back within the grace period.
	detach()
	detach() // twice is harmless
	time.Sleep(30 * time.Millisecond)
	again := s.AttachApp()
	select {
	case <-gone:
		t.Fatal("gone although the app came back")
	case <-time.After(300 * time.Millisecond):
	}
	start := time.Now()
	again()
	select {
	case ok := <-gone:
		if !ok {
			t.Fatal("WaitAppGone = false")
		}
		if d := time.Since(start); d < 90*time.Millisecond {
			t.Fatalf("gone after %v, before the grace period", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the app is closed, yet the daemon keeps waiting")
	}
}

func TestAppWatchEndsWithContext(t *testing.T) {
	s := &Service{}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan bool, 1)
	go func() { ch <- s.WaitAppGone(ctx, time.Hour, time.Hour) }()
	cancel()
	if <-ch {
		t.Fatal("WaitAppGone = true after cancel")
	}
}

func TestWaitAppAttached(t *testing.T) {
	s := &Service{}
	ch := make(chan bool, 1)
	go func() { ch <- s.WaitAppAttached(context.Background()) }()
	select {
	case <-ch:
		t.Fatal("attached before any app came")
	case <-time.After(100 * time.Millisecond):
	}
	detach := s.AttachApp()
	select {
	case ok := <-ch:
		if !ok {
			t.Fatal("WaitAppAttached = false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the app attached, yet still waiting")
	}
	// Already attached: at once.
	if !s.WaitAppAttached(context.Background()) {
		t.Fatal("WaitAppAttached = false while attached")
	}
	detach()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.WaitAppAttached(ctx) {
		t.Fatal("WaitAppAttached = true after cancel with no app")
	}
}
