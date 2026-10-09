package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/proc"
)

// A core whose executable an update replaced is started again on request,
// without leaving the connection: the new start is tried aside first, and
// one that does not work leaves the running core alone.
func TestRestartUpdatedCore(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.s.Restart(context.Background(), core.Xray, time.Now()); !errors.Is(err, ErrNotConnected) {
		t.Errorf("idle: err = %v, want ErrNotConnected", err)
	}
	before := time.Now()
	connect(t, h, trojanLink)
	first, _ := h.s.CoreListen()
	if err := h.s.Restart(context.Background(), core.SingBox, time.Now()); !errors.Is(err, ErrNotNeeded) {
		t.Errorf("another core: err = %v, want ErrNotNeeded", err)
	}
	if err := h.s.Restart(context.Background(), core.Xray, before); !errors.Is(err, ErrNotNeeded) {
		t.Errorf("started after the update: err = %v, want ErrNotNeeded", err)
	}

	// A new executable that does not start here.
	t.Setenv("FAKECORE_XRAY", "crash-start")
	if err := h.s.Restart(context.Background(), core.Xray, time.Now()); err == nil || errors.Is(err, ErrNotNeeded) {
		t.Errorf("broken update: err = %v", err)
	}
	if l, _ := h.s.CoreListen(); l != first || h.s.Status().State != Connected || len(h.s.Status().Failed) != 0 {
		t.Errorf("after a broken update: listen %v (was %v), status %+v", l, first, h.s.Status())
	}

	t.Setenv("FAKECORE_XRAY", "")
	h.drain()
	if err := h.s.Restart(context.Background(), core.Xray, time.Now()); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	st := h.s.Status()
	if l, _ := h.s.CoreListen(); l == first || st.State != Connected || st.Core != core.Xray || len(st.Failed) != 0 {
		t.Errorf("after the restart: listen %v (was %v), status %+v", l, first, st)
	}
	if !proc.PortOpen(h.listen) {
		t.Error("the SOCKS port was given up")
	}
	for _, e := range h.drain() {
		if e.Kind == EventSwap || e.Kind == EventCoreFailed || e.Kind == EventRestart {
			t.Errorf("event %+v", e)
		}
	}
	// The new start runs the current executable.
	if err := h.s.Restart(context.Background(), core.Xray, time.Now().Add(-time.Hour)); !errors.Is(err, ErrNotNeeded) {
		t.Errorf("again: err = %v, want ErrNotNeeded", err)
	}
}
