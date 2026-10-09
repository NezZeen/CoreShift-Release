//go:build !windows && !linux

package proc

import "net/netip"

func listens(int, netip.AddrPort) (bool, error) { return false, ErrOwnerUnknown }
