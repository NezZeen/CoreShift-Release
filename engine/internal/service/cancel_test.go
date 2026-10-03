package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"coreshift/engine/internal/proc"
)

// waitCall waits until the fakes were asked for what.
func waitCall(t *testing.T, h *harness, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !slices.Contains(h.log.get(), what) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened: %v", what, h.log.get())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func count(h *harness, what string) int {
	n := 0
	for _, c := range h.log.get() {
		if c == what {
			n++
		}
	}
	return n
}

// A connection stuck in a step (here the TUN layer, which never comes up)
// ends when the user disconnects, instead of holding Disconnect for the
// minutes the step may take.
func TestDisconnectCancelsConnect(t *testing.T) {
	h := newHarness(t, nil)
	h.tun.hang = true
	events, unsubscribe := h.svc.Subscribe(false)
	defer unsubscribe()
	done := make(chan error, 1)
	go func() { done <- h.connect(t, trojanLink) }()
	waitCall(t, h, "tun.start")

	start := time.Now()
	h.svc.Disconnect()
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Disconnect took %s", d)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrDisconnected) {
			t.Errorf("Connect = %v, want ErrDisconnected", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return after Disconnect")
	}
	if st := h.svc.Status(); st.State != Idle || st.Error != "" {
		t.Errorf("status = %+v, want idle without an error", st)
	}
	if proc.PortOpen(h.listen) {
		t.Error("the core still runs")
	}
	for {
		select {
		case e := <-events:
			if e.Kind == "error" || e.State == Failed {
				t.Errorf("a disconnect reported as a failure: %+v", e)
			}
			continue
		default:
		}
		break
	}
}

// A Connect that was still waiting for the one before it when the user
// disconnected does not start afterwards.
func TestDisconnectCancelsAWaitingConnect(t *testing.T) {
	h := newHarness(t, nil)
	h.tun.hang = true
	first := make(chan error, 1)
	go func() { first <- h.connect(t, trojanLink) }()
	waitCall(t, h, "tun.start")
	second := make(chan error, 1)
	go func() { second <- h.connect(t, trojanLink) }()
	time.Sleep(100 * time.Millisecond) // the second waits for the first

	h.svc.Disconnect()
	for _, ch := range []chan error{first, second} {
		select {
		case err := <-ch:
			if !errors.Is(err, ErrDisconnected) {
				t.Errorf("Connect = %v, want ErrDisconnected", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a Connect did not return after Disconnect")
		}
	}
	if n := count(h, "tun.start"); n != 1 {
		t.Errorf("the TUN layer was started %d times", n)
	}
	if st := h.svc.Status(); st.State != Idle {
		t.Errorf("status = %+v", st)
	}
	// Connecting works again afterwards.
	h.tun.hang = false
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
}

// Switching servers, which may try one server after another, stops at
// the user's Disconnect.
func TestDisconnectStopsAServerSwitch(t *testing.T) {
	h, _, _ := switchHarness(t, panelJSON, "proxy")
	h.tun.hang = true
	mark := count(h, "tun.start")
	serverNotAnswering(h)
	deadline := time.Now().Add(15 * time.Second)
	for count(h, "tun.start") == mark {
		if time.Now().After(deadline) {
			t.Fatal("no switch started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	h.svc.Disconnect()
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Disconnect took %s", d)
	}
	time.Sleep(300 * time.Millisecond)
	if n := count(h, "tun.start"); n != mark+1 {
		t.Errorf("%d more servers tried after Disconnect", n-mark-1)
	}
	if st := h.svc.Status(); st.State != Idle {
		t.Errorf("status = %+v", st)
	}
}

func TestBeginOpEndsWithItsOwner(t *testing.T) {
	h := newHarness(t, nil)
	ctx, end := h.svc.beginOp(context.Background())
	end()
	if ctx.Err() == nil {
		t.Error("the operation's context outlives it")
	}
	h.svc.mu.Lock()
	left := h.svc.opCancel
	h.svc.mu.Unlock()
	if left != nil {
		t.Error("the cancel func of a finished operation stays")
	}
}
