//go:build linux && !android

package service

import (
	"os"
	"strings"
)

// ipv6Disabled reports whether the system refuses IPv6 on new interfaces:
// net.ipv6.conf.default.disable_ipv6 = 1 (many guides and hardening
// scripts set it), or IPv6 switched off at boot (ipv6.disable=1), when
// /proc/sys/net/ipv6 is missing. Given an IPv6 address there, the TUN
// interface fails to start ("add address …: permission denied") and so
// would the whole connection.
func ipv6Disabled() bool { return ipv6DisabledIn("/proc/sys/net/ipv6") }

func ipv6DisabledIn(dir string) bool {
	b, err := os.ReadFile(dir + "/conf/default/disable_ipv6")
	if err != nil {
		return os.IsNotExist(err)
	}
	return strings.TrimSpace(string(b)) == "1"
}
