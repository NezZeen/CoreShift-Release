package socksgate

import (
	"net"
	"net/netip"
	"testing"
	"time"
)

// Connections past the cap are closed on arrival; the ones inside it keep
// their place, and a place freed is taken again.
func TestGateTurnsAwayConnectionsPastTheCap(t *testing.T) {
	const max = 4
	g, err := Listen(Config{Listen: netip.MustParseAddrPort("127.0.0.1:0"), Open: true, MaxSessions: max})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close() })
	dial := func() net.Conn {
		c, err := net.Dial("tcp", g.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	held := make([]net.Conn, 0, max)
	for range max {
		held = append(held, dial())
	}
	waitSessions := func(n int) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if g.Sessions() == n {
				return
			}
		}
		t.Fatalf("sessions = %d, want %d", g.Sessions(), n)
	}
	waitSessions(max)

	extra := dial()
	extra.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := extra.Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Fatalf("the connection past the cap was kept: %v", err)
	}
	if g.Sessions() != max {
		t.Errorf("sessions = %d, want %d", g.Sessions(), max)
	}

	held[0].Close()
	waitSessions(max - 1)
	again := dial()
	again.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := again.Read(make([]byte, 1)); err == nil || !isTimeout(err) {
		t.Errorf("a freed place was not given again: %v", err)
	}
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}
