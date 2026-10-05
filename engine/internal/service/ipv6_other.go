//go:build (!linux && !windows) || android

package service

// ipv6Disabled: Android's VpnService takes the address itself.
func ipv6Disabled() bool { return false }
