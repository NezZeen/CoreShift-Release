package service

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/tunlayer"
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

// updTUN is the desktop's TUN layer as far as updates go: a sing-box
// process of its own (tunUpdatable).
type updTUN struct {
	*fakeTUN
	checkErr error
}

func (u *updTUN) Check(context.Context, tunlayer.Options) error {
	u.log.add("tun.check")
	return u.checkErr
}

func (u *updTUN) StartPrevious(ctx context.Context, o tunlayer.Options) (TUNInstance, error) {
	u.log.add("tun.previous")
	return u.fakeTUN.Start(ctx, o)
}

// desktopTUN makes h's TUN layer the desktop's; before connecting.
func desktopTUN(h *harness, checkErr error) {
	h.svc.cfg.tun = &updTUN{fakeTUN: h.tun, checkErr: checkErr}
}

// failTUNStarts fails the next n starts of h's TUN layer.
func failTUNStarts(h *harness, n int) {
	h.tun.mu.Lock()
	h.tun.startErr, h.tun.failStarts = errors.New("configure tun interface: create adapter: busy"), n
	h.tun.mu.Unlock()
}

// linkGuard is a DNS guard bound to the TUN interface, as Linux's is.
type linkGuard struct{ *fakeGuard }

func (linkGuard) LinkBound() {}

// tunEvent returns the first event match takes, or false when none comes
// within d.
func tunEvent(h *harness, d time.Duration, match func(Event) bool) (Event, bool) {
	end := time.After(d)
	for {
		select {
		case e := <-h.events:
			if match(e) {
				return e, true
			}
		case <-end:
			return Event{}, false
		}
	}
}

func tunUpdated(e Event) bool { return e.Kind == "tun" && e.Reason == "updated" }

func tunError(e Event) bool { return e.Kind == "tun" && e.Error != "" }

// A sing-box update reaches the desktop's TUN layer, a sing-box process of
// its own whichever core serves: once the connection is quiet the layer is
// started again on the new version, with the same options, without
// disconnecting. The new version checks the config before the old layer
// stops, and the system DNS stays pointed into the tunnel throughout.
func TestSingBoxUpdateMovesTheTUNLayer(t *testing.T) {
	h := newHarness(t, quickApply)
	desktopTUN(h, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	first, _ := h.svc.sup.CoreListen()
	old, opts := h.tun.instance(), h.tun.opts
	from := len(h.log.get())
	h.svc.noteInstalled(core.SingBox, "1.14.4")
	e, ok := tunEvent(h, 10*time.Second, tunUpdated)
	if !ok {
		t.Fatalf("the TUN layer did not move; calls %v", h.log.get()[from:])
	}
	if e.Line != "1.14.4" || e.Core != string(core.SingBox) {
		t.Errorf("event %+v", e)
	}
	applyDone(t, h)
	if calls := h.log.get()[from:]; !slices.Equal(calls, []string{"tun.check", "tun.stop", "tun.start"}) {
		t.Errorf("calls %v", calls)
	}
	if h.tun.instance() == old || !reflect.DeepEqual(h.tun.opts, opts) {
		t.Error("not started again with the same options")
	}
	// The old layer's exit is not taken for a TUN layer that died.
	time.Sleep(300 * time.Millisecond)
	l, _ := h.svc.sup.CoreListen()
	if st := h.svc.Status(); st.State != Connected || l != first || h.guard.active() == nil {
		t.Errorf("after the move: status %+v, core listen %v (was %v), DNS %v", st, l, first, h.guard.active())
	}
}

// With sing-box the core as well, the core moves first, then the TUN layer
// in the same quiet moment.
func TestSingBoxUpdateMovesCoreThenTUN(t *testing.T) {
	h := newHarness(t, quickApply)
	desktopTUN(h, nil)
	if err := h.connect(t, hy2Link); err != nil {
		t.Fatal(err)
	}
	if st := h.svc.Status(); st.Core != core.SingBox {
		t.Fatalf("status %+v", st)
	}
	first, _ := h.svc.sup.CoreListen()
	h.svc.noteInstalled(core.SingBox, "1.14.4")
	var got []string
	deadline := time.After(10 * time.Second)
	for len(got) < 2 {
		select {
		case e := <-h.events:
			if e.Kind == "cores" && e.Reason == "applied" {
				got = append(got, "core")
			} else if tunUpdated(e) {
				got = append(got, "tun")
			}
		case <-deadline:
			t.Fatalf("got %v", got)
		}
	}
	if !slices.Equal(got, []string{"core", "tun"}) {
		t.Errorf("order %v", got)
	}
	applyDone(t, h)
	if l, _ := h.svc.sup.CoreListen(); l == first || h.svc.Status().State != Connected {
		t.Errorf("listen %v (was %v), status %+v", l, first, h.svc.Status())
	}
}

// Where the guard's changes go with the interface (Linux), they are made
// again on the new one; the system never gets its DNS back meanwhile.
func TestTUNMoveReappliesLinkBoundGuard(t *testing.T) {
	h := newHarness(t, quickApply)
	desktopTUN(h, nil)
	h.svc.cfg.guard = linkGuard{h.guard}
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	from := len(h.log.get())
	h.svc.noteInstalled(core.SingBox, "1.14.4")
	if _, ok := tunEvent(h, 10*time.Second, tunUpdated); !ok {
		t.Fatalf("calls %v", h.log.get()[from:])
	}
	if calls := h.log.get()[from:]; !slices.Equal(calls, []string{"tun.check", "tun.stop", "tun.start", "dns.apply"}) {
		t.Errorf("calls %v", calls)
	}
}

// A new version that refuses the layer's config never takes the interface
// down: the layer stays on the old one, and the journal says so.
func TestTUNMoveRefusedKeepsTheLayer(t *testing.T) {
	h := newHarness(t, quickApply)
	desktopTUN(h, errors.New(`decode config: unknown field "sniff"`))
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	old := h.tun.instance()
	from := len(h.log.get())
	h.svc.noteInstalled(core.SingBox, "1.14.4")
	e, ok := tunEvent(h, 10*time.Second, tunError)
	if !ok || !strings.Contains(e.Error, "до следующего подключения") || !strings.Contains(e.Error, "sniff") {
		t.Fatalf("event %+v", e)
	}
	applyDone(t, h)
	if calls := h.log.get()[from:]; !slices.Equal(calls, []string{"tun.check"}) || h.tun.instance() != old || h.svc.Status().State != Connected {
		t.Errorf("calls %v, status %+v", calls, h.svc.Status())
	}
}

// A new version that does not start falls back to the one it replaced,
// without reconnecting.
func TestTUNMoveFallsBackToPreviousVersion(t *testing.T) {
	h := newHarness(t, quickApply)
	desktopTUN(h, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	first, _ := h.svc.sup.CoreListen()
	failTUNStarts(h, 1)
	from := len(h.log.get())
	h.svc.noteInstalled(core.SingBox, "1.14.4")
	e, ok := tunEvent(h, 10*time.Second, tunError)
	if !ok || !strings.Contains(e.Error, "вернул прежнюю версию") {
		t.Fatalf("event %+v", e)
	}
	applyDone(t, h)
	calls := h.log.get()[from:]
	if !slices.Equal(calls, []string{"tun.check", "tun.stop", "tun.start", "tun.previous", "tun.start"}) {
		t.Errorf("calls %v", calls)
	}
	time.Sleep(300 * time.Millisecond)
	if l, _ := h.svc.sup.CoreListen(); l != first || h.svc.Status().State != Connected || h.guard.active() == nil {
		t.Errorf("listen %v (was %v), status %+v", l, first, h.svc.Status())
	}
}

// When neither version starts, the connection is made anew, as after a TUN
// layer that died.
func TestTUNMoveReconnectsWhenNothingStarts(t *testing.T) {
	h := newHarness(t, quickApply)
	desktopTUN(h, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	failTUNStarts(h, 2)
	h.svc.noteInstalled(core.SingBox, "1.14.4")
	e, ok := tunEvent(h, 10*time.Second, tunError)
	if !ok || !strings.Contains(e.Error, "переподключаюсь") {
		t.Fatalf("event %+v", e)
	}
	applyDone(t, h)
	if st := h.svc.Status(); st.State != Connected || h.guard.active() == nil || h.tun.instance() == nil {
		t.Errorf("status %+v", st)
	}
}

// Disconnect does not wait for a new start that hangs (an adapter Windows
// has not freed), and the connection ends as the user asked, not failed.
func TestDisconnectCancelsTUNMove(t *testing.T) {
	h := newHarness(t, quickApply)
	desktopTUN(h, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	from := len(h.log.get())
	h.tun.hang = true
	h.svc.noteInstalled(core.SingBox, "1.14.4")
	for deadline := time.Now().Add(10 * time.Second); !slices.Contains(h.log.get()[from:], "tun.start"); {
		if time.Now().After(deadline) {
			t.Fatalf("calls %v", h.log.get()[from:])
		}
		time.Sleep(20 * time.Millisecond)
	}
	done := make(chan struct{})
	go func() { h.svc.Disconnect(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect waits for the TUN layer's start")
	}
	applyDone(t, h)
	time.Sleep(200 * time.Millisecond)
	if st := h.svc.Status(); st.State != Idle || h.guard.active() != nil {
		t.Errorf("status %+v, DNS %v", st, h.guard.active())
	}
}

// Android's TUN layer runs inside the app: a sing-box update leaves it.
func TestSingBoxUpdateLeavesInAppTUN(t *testing.T) {
	h := newHarness(t, quickApply)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	from := len(h.log.get())
	h.svc.noteInstalled(core.SingBox, "1.14.4")
	applyDone(t, h)
	if e, ok := tunEvent(h, 300*time.Millisecond, func(e Event) bool { return tunUpdated(e) || tunError(e) }); ok {
		t.Errorf("event %+v", e)
	}
	if calls := h.log.get()[from:]; len(calls) != 0 || h.svc.Status().State != Connected {
		t.Errorf("calls %v, status %+v", calls, h.svc.Status())
	}
}
