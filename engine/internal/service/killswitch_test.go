package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/proc"
)

// killHarness is a harness with the kill switch on and quick retries.
func killHarness(t *testing.T) *harness {
	t.Helper()
	return newHarness(t, func(c *Config) {
		c.Options.KillSwitch = true
		c.blockedRetry = 100 * time.Millisecond
	})
}

func failEveryCore(t *testing.T) {
	t.Helper()
	t.Setenv("FAKECORE_XRAY", "crash-after:700ms")
	t.Setenv("FAKECORE_SING_BOX", "crash-start")
	t.Setenv("FAKECORE_MIHOMO", "crash-start")
}

// With the kill switch on, every core failing leaves the TUN layer, the DNS
// redirect and the server's route in place: Blocked, not Failed.
func TestKillSwitchHoldsTheLayerWhenEveryCoreFails(t *testing.T) {
	failEveryCore(t)
	h := killHarness(t)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	mark := len(h.log.get())
	st := h.waitState(t, Blocked, 10*time.Second)
	if !st.TUN || st.Error == "" || !st.State.active() {
		t.Errorf("status = %+v", st)
	}
	// Retries run meanwhile; the layer never goes down under them.
	time.Sleep(600 * time.Millisecond)
	calls := h.log.get()[mark:]
	for _, bad := range []string{"dns.revert", "tun.stop"} {
		if slices.Contains(calls, bad) {
			t.Errorf("%s while blocked: %v", bad, calls)
		}
	}
	if h.guard.active() == nil {
		t.Error("the DNS redirect was lifted while blocked")
	}
	h.mu.Lock()
	routes := len(h.routes)
	h.mu.Unlock()
	if routes != 1 {
		t.Errorf("the server's route is gone while blocked: %d routes", routes)
	}
}

// The user's Disconnect always tears everything down, blocked or not.
func TestDisconnectEndsTheBlock(t *testing.T) {
	failEveryCore(t)
	h := killHarness(t)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, Blocked, 10*time.Second)
	mark := len(h.log.get())
	h.svc.Disconnect()
	if st := h.svc.Status(); st.State != Idle {
		t.Fatalf("status = %+v", st)
	}
	calls := h.log.get()[mark:]
	if !slices.Contains(calls, "dns.revert") || !slices.Contains(calls, "tun.stop") || h.guard.active() != nil {
		t.Errorf("not torn down: %v", calls)
	}
	if proc.PortOpen(h.listen) {
		t.Error("core left running")
	}
	h.svc.mu.Lock()
	blocked, cancel := h.svc.blocked, h.svc.blockCancel
	h.svc.mu.Unlock()
	if blocked || cancel != nil {
		t.Error("the block outlived the disconnect")
	}
	// No retry may bring it back.
	time.Sleep(400 * time.Millisecond)
	if st := h.svc.Status(); st.State != Idle {
		t.Errorf("status after the retries' pause = %+v", st)
	}
}

// Turning the setting off while blocked lets the next retry's failure tear
// the connection down as it always did.
func TestKillSwitchTurnedOffWhileBlocked(t *testing.T) {
	failEveryCore(t)
	h := killHarness(t)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, Blocked, 10*time.Second)
	o := h.svc.Options()
	o.KillSwitch = false
	h.svc.SetOptions(o)
	h.waitState(t, Failed, 10*time.Second)
	if h.guard.active() != nil || !slices.Contains(h.log.get(), "tun.stop") {
		t.Errorf("not torn down: %v", h.log.get())
	}
}

// A TUN layer that dies is built again under the kept DNS redirect, and the
// connection is Connected once it is.
func TestKillSwitchRestartsTheDeadTUN(t *testing.T) {
	h := killHarness(t)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, c := range h.log.get() {
		if c == "tun.start" {
			starts++
		}
	}
	h.tun.instance().crash()
	// Blocked is brief when the cores are fine: wait for the rebuilt layer.
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := 0
		for _, c := range h.log.get() {
			if c == "tun.start" {
				n++
			}
		}
		if n > starts && h.svc.Status().State == Connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the layer was not restarted; status %+v, calls %v", h.svc.Status(), h.log.get())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if h.guard.active() == nil {
		t.Error("no DNS redirect after the restart")
	}
}

// Without the setting a dying TUN layer still ends in Failed with DNS back
// (TestTUNCrashRestoresDNS); a layer the system took away is never held.
func TestKillSwitchDoesNotHoldARevokedTUN(t *testing.T) {
	h := killHarness(t)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.svc.op.Lock()
	held := h.svc.holdBlocked(fmt.Errorf("android turned the VPN off: %w", ErrTUNRevoked))
	h.svc.op.Unlock()
	if held {
		t.Error("held a revoked VPN")
	}
	// Any other cause is held.
	h.svc.op.Lock()
	held = h.svc.holdBlocked(errors.New("every compatible core failed"))
	h.svc.op.Unlock()
	if !held || h.svc.Status().State != Blocked {
		t.Errorf("held = %v, status %+v", held, h.svc.Status())
	}
}

// A connection that never came up is no reason to block anything: the user
// has had no VPN to lose.
func TestKillSwitchIgnoresAFirstConnectionThatFails(t *testing.T) {
	h := killHarness(t)
	h.tun.startErr = errors.New("wintun: access denied")
	err := h.connect(t, trojanLink)
	if err == nil || !strings.Contains(err.Error(), "TUN") {
		t.Fatalf("err = %v", err)
	}
	if st := h.svc.Status(); st.State != Failed {
		t.Errorf("status = %+v", st)
	}
	if h.guard.active() != nil {
		t.Error("DNS left redirected")
	}
}
