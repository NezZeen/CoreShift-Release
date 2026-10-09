package proc

import (
	"errors"
	"net/netip"
)

// ErrOwnerUnknown means the system does not say who listens on a port:
// on Android, from version 10, apps cannot read the socket tables.
var ErrOwnerUnknown = errors.New("the listening socket's owner cannot be told")

// Pid is the process's ID.
func (p *Process) Pid() int { return p.cmd.Process.Pid }

// Listens reports whether the TCP listeners that take connections to addr
// (on addr itself, or on the wildcard address of its port) all belong to
// the process pid. False when another process holds one of them, or when
// nothing listens. A port chosen at random is free a moment before the
// core opens it, and another program that took it in that moment would
// receive the traffic sent there; this tells the two apart.
// ErrOwnerUnknown where the system does not say.
func Listens(pid int, addr netip.AddrPort) (bool, error) { return listens(pid, addr) }

// matches reports whether a listener on local takes connections to addr.
func matches(local, addr netip.AddrPort) bool {
	if local.Port() != addr.Port() {
		return false
	}
	a := local.Addr().Unmap()
	return a == addr.Addr().Unmap() || a.IsUnspecified()
}
