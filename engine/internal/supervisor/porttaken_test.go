package supervisor

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

func TestPortWatchMatchesEveryCoresBindError(t *testing.T) {
	addr := netip.MustParseAddrPort("127.0.0.1:17890")
	for _, line := range []string{
		// mihomo, which keeps running
		`level=error msg="Listener socks-in listen err: listen tcp 127.0.0.1:17890: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted."`,
		// xray
		`Failed to start: app/proxyman/inbound: failed to listen TCP on 17890 > listen tcp 127.0.0.1:17890: bind: address already in use`,
		// sing-box
		`FATAL[0000] start service: start inbound/socks[socks-in]: listen tcp 127.0.0.1:17890: bind: address already in use`,
	} {
		w := newPortWatch(addr)
		w.line(line)
		if !w.failed() {
			t.Errorf("not recognised: %s", line)
		}
	}
	for _, line := range []string{
		`level=info msg="SOCKS proxy listening at: 127.0.0.1:17890"`,
		`INFO inbound/socks[socks-in]: tcp server started at 127.0.0.1:17890`,
		// Another port's trouble is not this core's.
		`listen tcp 127.0.0.1:17891: bind: address already in use`,
	} {
		w := newPortWatch(addr)
		w.line(line)
		if w.failed() {
			t.Errorf("taken for a lost port: %s", line)
		}
	}
}

// A core that says its port is taken but keeps running (mihomo) must not
// count as started: what answers on the port is not the core.
func TestCoreThatCannotOpenItsPortIsDropped(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "port-taken")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	st := h.s.Status()
	if st.Core != core.SingBox || !strings.Contains(st.Failed[core.Xray], "another program holds it") {
		t.Fatalf("status = %+v", st)
	}
}

func TestCoreThatLosesItsPortIsReplaced(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "port-taken-after:500ms")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	if st := h.s.Status(); st.Core != core.Xray {
		t.Fatalf("status = %+v", st)
	}
	h.waitFor(t, "swap to sing-box", 10*time.Second, isSwap(core.SingBox, ReasonExited))
	if st := h.s.Status(); !strings.Contains(st.Failed[core.Xray], "another program holds it") {
		t.Fatalf("status = %+v", st)
	}
}

// Another program that holds a core's random port before the core opens
// it, accepting any credentials, is told apart from the core by the
// system's socket table and receives nothing: the core counts as failed
// to start, and the connection goes to the next one.
func TestImpostorOnTheCorePortIsRefused(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "impostor")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	st := h.s.Status()
	if st.Core != core.SingBox || !strings.Contains(st.Failed[core.Xray], "another program holds it") {
		t.Fatalf("status = %+v", st)
	}
	if err := getThrough(h.s.SOCKSAuth().ProxyURL(h.listen)); err != nil {
		t.Fatalf("through the SOCKS port: %v", err)
	}
}
