package service

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestRealRouteTableHoldsAndResumes takes the default route of a real
// network namespace away and gives it back, with the route table read as
// the daemon reads it. Run as root inside a throwaway namespace with a
// default route (CORESHIFT_NETNS_TEST=<gateway> <device>), e.g.
//
//	ip netns exec csnl env CORESHIFT_NETNS_TEST="10.77.0.1 csa" ./service.test -test.run RealRoute
func TestRealRouteTableHoldsAndResumes(t *testing.T) {
	spec := os.Getenv("CORESHIFT_NETNS_TEST")
	if spec == "" {
		t.Skip("needs a network namespace of its own: CORESHIFT_NETNS_TEST")
	}
	var gw, dev string
	if _, err := fmt.Sscan(spec, &gw, &dev); err != nil {
		t.Fatalf("CORESHIFT_NETNS_TEST=%q: %v", spec, err)
	}
	route := func(op string) {
		t.Helper()
		if out, err := exec.Command("ip", "route", op, "default", "via", gw, "dev", dev).CombinedOutput(); err != nil {
			t.Fatalf("ip route %s: %v: %s", op, err, out)
		}
	}
	h, nw := newNetRig(t, func(c *Config) { c.netUp = hasNetwork })
	if !hasNetwork() {
		t.Fatal("no default route to start with")
	}
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	// The cable is pulled: the route goes, every check fails.
	nw.down(t)
	h.offline.Store(false) // only the route table tells
	route("del")
	defer route("replace")
	start := time.Now()
	st := h.waitState(t, NoNetwork, 10*time.Second)
	t.Logf("held %v after the route went: %+v", time.Since(start).Round(time.Millisecond), st)
	if h.guard.active() == nil {
		t.Error("the DNS redirect went while held")
	}
	time.Sleep(time.Second)
	if st := h.svc.Status(); st.State != NoNetwork {
		t.Fatalf("not held: %+v", st)
	}
	// It is back.
	route("replace")
	os.Remove(nw.file)
	start = time.Now()
	h.waitState(t, Connected, 10*time.Second)
	t.Logf("connected %v after the route came back", time.Since(start).Round(time.Millisecond))

	// Connecting while the route is gone waits, and connects once it is back.
	h.svc.Disconnect()
	route("del")
	if err := h.connect(t, namedLink); err != nil {
		t.Fatalf("connect without a route: %v", err)
	}
	if st := h.svc.Status(); st.State != NoNetwork || !st.Waiting {
		t.Fatalf("status = %+v", st)
	}
	route("replace")
	h.waitState(t, Connected, 10*time.Second)
}
