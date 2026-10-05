package service

import (
	"net/netip"
	"testing"

	"coreshift/engine/internal/ping"
)

// Pings leave by the physical interface, except to this computer itself:
// bound to it, a probe of a server on 127.0.0.1 timed out (seen on Linux,
// "dial tcp 10.214.0.2:0->127.0.0.1:18381: i/o timeout"), so the server
// showed no latency and the switch to another server passed it over.
func TestBindForLeavesLoopbackAlone(t *testing.T) {
	phys := ping.Bind{Source: netip.MustParseAddr("192.168.1.23"), Interface: "eth0"}
	for _, c := range []struct {
		ip   string
		want ping.Bind
	}{
		{"203.0.113.7", phys},
		{"192.168.1.1", phys},
		{"127.0.0.1", ping.Bind{}},
		{"::ffff:127.0.0.1", ping.Bind{}},
		{"::1", ping.Bind{}},
		{"2001:db8::1", ping.Bind{}}, // another family than the source
	} {
		if got := bindFor(phys, netip.MustParseAddr(c.ip)); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.ip, got, c.want)
		}
	}
}
