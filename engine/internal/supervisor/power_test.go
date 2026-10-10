package supervisor

import (
	"path/filepath"
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

// The pace of the checks: the user's interval, the idle pace while the
// device is idle, and the floor of battery saver over both.
func TestHealthEvery(t *testing.T) {
	s := &Supervisor{cfg: Config{Health: Health{Interval: 15 * time.Second}, IdleHealthInterval: time.Minute}}
	cases := []struct {
		idle  bool
		floor time.Duration
		want  time.Duration
	}{
		{false, 0, 15 * time.Second},
		{true, 0, time.Minute},
		{false, 30 * time.Second, 30 * time.Second},
		{true, 5 * time.Minute, 5 * time.Minute},
		// A floor below the pace changes nothing.
		{false, 5 * time.Second, 15 * time.Second},
		{true, 30 * time.Second, time.Minute},
	}
	for _, c := range cases {
		s.idle.Store(c.idle)
		s.SetHealthFloor(c.floor)
		if got := s.healthEvery(); got != c.want {
			t.Errorf("idle %v, floor %v: every %v, want %v", c.idle, c.floor, got, c.want)
		}
	}
	s.SetHealthFloor(-time.Second)
	s.idle.Store(false)
	if got := s.healthEvery(); got != 15*time.Second {
		t.Errorf("negative floor: every %v", got)
	}
}

// Under a floor a healthy connection is checked seldom, and a server that
// stops answering is still left at the usual pace of failed checks.
func TestHealthFloorKeepsFailuresQuick(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "unhealthy-after:2s")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	h.waitFor(t, "first check", 2*time.Second, func(e Event) bool { return e.Kind == EventHealth })
	h.s.SetHealthFloor(700 * time.Millisecond)
	h.drain()
	time.Sleep(1500 * time.Millisecond)
	ok, _ := healthChecks(h.drain())
	// Every 100 ms without it: 15 checks; with it, every 700 ms.
	if ok < 1 || ok > 3 {
		t.Errorf("%d checks in 1.5 s under the floor, want 2", ok)
	}
	h.waitFor(t, "swap under the floor", 10*time.Second, isSwap(core.SingBox, ReasonHealth))
}

// While the device is idle the return to the primary core waits: no second
// core is started aside. In use again, it comes soon after.
func TestReturnToPrimaryWaitsWhileIdle(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "xray-crashed-once")
	t.Setenv("FAKECORE_XRAY", "crash-start-once:"+marker)
	h := newHarness(t, func(c *Config) { c.ReturnToPrimaryAfter = 300 * time.Millisecond })
	h.s.SetIdle(true)
	connect(t, h, trojanLink)
	if st := h.s.Status(); st.Core != core.SingBox {
		t.Fatalf("expected to start on the backup, status = %+v", st)
	}
	time.Sleep(1500 * time.Millisecond)
	for _, e := range h.drain() {
		if e.Probe || isSwap(core.Xray, ReasonReturn)(e) {
			t.Fatalf("while idle: %s %s (probe %v)", e.Kind, e.Core, e.Probe)
		}
	}
	woke := time.Now()
	h.s.SetIdle(false)
	h.waitFor(t, "return to xray", 15*time.Second, isSwap(core.Xray, ReasonReturn))
	if d := time.Since(woke); d < backOnWake/2 {
		t.Errorf("returned %v after waking, before the wake's own check", d)
	}
}
