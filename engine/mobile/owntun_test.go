package mobile

import (
	"net/netip"
	"testing"
)

func prefixes(s ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, v := range s {
		out = append(out, netip.MustParsePrefix(v))
	}
	return out
}

// What the app's network callback reported in the emulator once the VPN
// was up: the VPN's own link, not the phone's network.
func TestOwnVPNIsNotThePhonesNetwork(t *testing.T) {
	for name, c := range map[string]struct {
		name  string
		addrs []netip.Prefix
		mine  []string
		want  bool
	}{
		"the VPN by its addresses":   {"tun0", prefixes("172.19.0.1/30", "fdfe:dcba:9876::1/126"), nil, true},
		"the VPN by its name":        {"tun1", nil, []string{"tun1"}, true},
		"Wi-Fi":                      {"wlan0", prefixes("192.168.1.23/24", "fe80::1/64"), []string{"tun0"}, false},
		"mobile data":                {"rmnet_data0", prefixes("10.47.3.9/30", "2a00:1fa0:1::5/64"), []string{"tun0"}, false},
		"the emulator's mobile data": {"eth0", prefixes("10.0.2.15/24"), nil, false},
	} {
		if got := ownVPN(c.name, c.addrs, c.mine); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
