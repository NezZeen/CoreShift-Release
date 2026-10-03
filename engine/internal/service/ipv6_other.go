//go:build !linux || android

package service

// ipv6Disabled: Windows gives the TUN interface IPv6 whatever the network
// has, and Android's VpnService takes the address itself.
func ipv6Disabled() bool { return false }
