package ping

import (
	"errors"
	"net"
	"testing"
)

const procRouteV4 = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
wlan0	00000000	0101A8C0	0003	0	0	600	00000000	0	0	0
wlan0	0001A8C0	00000000	0001	0	0	600	00FFFFFF	0	0	0
`

const procRouteV4NoDefault = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
docker0	000011AC	00000000	0001	0	0	0	0000FFFF	0	0	0
*	00000000	00000000	0201	0	0	0	00000000	0	0	0
`

const procRouteV6 = `00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000001 00000000 00450003 eth0
fe800000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000001 00000000 00000001 eth0
00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200 lo
00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000400 00000001 00000000 00000001 coreshift
`

func TestProcDefaultRoute(t *testing.T) {
	all := func(string) bool { return true }
	notTUN := func(name string) bool { return name != "coreshift" }
	noEth := func(name string) bool { return name != "eth0" && name != "coreshift" }
	for name, c := range map[string]struct {
		data   string
		v6     bool
		usable func(string) bool
		want   bool
	}{
		"Wi-Fi default route":                 {procRouteV4, false, all, true},
		"the interface is down":               {procRouteV4, false, func(string) bool { return false }, false},
		"only a LAN and an unreachable route": {procRouteV4NoDefault, false, all, false},
		"IPv6 default route":                  {procRouteV6, true, notTUN, true},
		// The loopback's reject route and the tunnel's own do not count.
		"IPv6 without the physical one": {procRouteV6, true, noEth, false},
		"empty":                         {"", false, all, false},
	} {
		if got := procDefaultRoute(c.data, c.v6, c.usable) != ""; got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func TestUsableInterface(t *testing.T) {
	up := net.FlagUp | net.FlagRunning
	for name, c := range map[string]struct {
		ifc  net.Interface
		want bool
	}{
		"up with a link":   {net.Interface{Name: "eth0", Flags: up}, true},
		"no link":          {net.Interface{Name: "eth0", Flags: net.FlagUp}, false},
		"down":             {net.Interface{Name: "eth0"}, false},
		"loopback":         {net.Interface{Name: "lo", Flags: up | net.FlagLoopback}, false},
		"the engine's TUN": {net.Interface{Name: "coreshift", Flags: up}, false},
	} {
		if got := usableInterface(&c.ifc, []string{"coreshift"}); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// The real table, for what it is worth here: a machine running the tests
// has a network or does not, but asking must not fail.
func TestHasDefaultRoute(t *testing.T) {
	ok, err := HasDefaultRoute("coreshift")
	if err != nil && !errors.Is(err, ErrUnsupported) {
		t.Fatalf("HasDefaultRoute: %v", err)
	}
	t.Logf("default route: %v", ok)
}
