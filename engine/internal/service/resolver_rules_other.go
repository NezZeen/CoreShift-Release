//go:build !linux

package service

import "net/netip"

// addResolverRules does nothing: only Linux routes the local network
// around the TUN (tunlayer.Options.ExcludeLAN).
func addResolverRules([]netip.Addr, func(string)) func() { return func() {} }
