package service

import "testing"

func TestIPv6DisabledBy(t *testing.T) {
	for v, want := range map[uint64]bool{
		0:          false,
		0x01:       false, // tunnel interfaces only: Wintun is not one
		0x20:       false, // IPv4 preferred, IPv6 still there
		0x10:       true,
		0x11:       true,
		0xFF:       true,
		0xFFFFFFFF: true,
	} {
		if got := ipv6DisabledBy(v); got != want {
			t.Errorf("DisabledComponents %#x: %v, want %v", v, got, want)
		}
	}
	ipv6Disabled() // reads the registry without failing
}
