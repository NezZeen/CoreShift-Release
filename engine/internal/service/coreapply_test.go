package service

import (
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

func quickApply(c *Config) {
	c.coreApplyEvery, c.coreApplyQuiet = 50*time.Millisecond, 400*time.Millisecond
}

// applyDone waits for applyCores to end.
func applyDone(t *testing.T, h *harness) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; {
		h.svc.cores.mu.Lock()
		running := h.svc.cores.applying != 0
		h.svc.cores.mu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("applyCores still runs")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitApplied returns the "applied" event, or false when none comes
// within d.
func waitApplied(h *harness, d time.Duration) (Event, bool) {
	end := time.After(d)
	for {
		select {
		case e := <-h.events:
			if e.Kind == "cores" && e.Reason == "applied" {
				return e, true
			}
		case <-end:
			return Event{}, false
		}
	}
}

// The running core, updated, moves to the new executable once the
// connection is quiet, without disconnecting and without asking the user
// to reconnect; not while traffic flows.
func TestUpdatedCoreAppliedWhenQuiet(t *testing.T) {
	h := newHarness(t, quickApply)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	st := h.svc.Status()
	if st.Core != core.Xray {
		t.Fatalf("status %+v", st)
	}
	first, _ := h.svc.sup.CoreListen()
	stop := h.browse(t)
	h.svc.noteInstalled(core.Xray, "1.3.0")
	if h.svc.Status().Pending {
		t.Error("a core update asks for a reconnect")
	}
	if e, ok := waitApplied(h, 1500*time.Millisecond); ok {
		t.Fatalf("applied while traffic flows: %+v", e)
	}
	if l, _ := h.svc.sup.CoreListen(); l != first {
		t.Fatal("the core restarted while traffic flows")
	}

	stop()
	e, ok := waitApplied(h, 10*time.Second)
	if !ok {
		t.Fatalf("not applied once quiet; status %+v", h.svc.Status())
	}
	if e.Core != string(core.Xray) || e.Line != "1.3.0" {
		t.Errorf("event %+v", e)
	}
	st = h.svc.Status()
	if l, _ := h.svc.sup.CoreListen(); l == first || st.State != Connected || st.Core != core.Xray || st.Pending {
		t.Errorf("after applying: listen %v (was %v), status %+v", l, first, st)
	}
	applyDone(t, h)
	// The connection carries traffic again.
	h.browse(t)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e := <-h.events:
			if e.Kind == kindTraffic && e.DownRate > 0 {
				return
			}
		case <-deadline:
			t.Fatal("no traffic after applying")
		}
	}
}

// An update of a core that does not run now changes nothing in the
// connection: its next start runs the new executable.
func TestUpdateOfAnotherCoreLeavesTheConnection(t *testing.T) {
	h := newHarness(t, quickApply)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	first, _ := h.svc.sup.CoreListen()
	h.svc.noteInstalled(core.Mihomo, "1.20.0")
	applyDone(t, h)
	if e, ok := waitApplied(h, 600*time.Millisecond); ok {
		t.Fatalf("event %+v", e)
	}
	if l, _ := h.svc.sup.CoreListen(); l != first || h.svc.Status().Pending {
		t.Errorf("listen %v (was %v), status %+v", l, first, h.svc.Status())
	}
}

// Nothing waits once the connection ends: the next one starts the new
// executable. Nor is anything left to apply without a connection.
func TestCoreApplyEndsWithTheConnection(t *testing.T) {
	h := newHarness(t, func(c *Config) { quickApply(c); c.coreApplyQuiet = time.Hour })
	h.svc.noteInstalled(core.Xray, "1.3.0")
	applyDone(t, h)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.svc.noteInstalled(core.Xray, "1.3.0")
	time.Sleep(200 * time.Millisecond)
	h.svc.Disconnect()
	applyDone(t, h)
	if e, ok := waitApplied(h, 200*time.Millisecond); ok {
		t.Fatalf("event %+v", e)
	}
}
