package socksgate

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// soExclusiveAddrUse is SO_EXCLUSIVEADDRUSE (~SO_REUSEADDR in winsock2.h).
const soExclusiveAddrUse = ^4

// exclusive keeps the port the gate's alone. Without it Windows lets
// another socket with SO_REUSEADDR bind the same address and port, and
// share in, or take over, the connections meant for the gate.
func exclusive(_, _ string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		serr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, soExclusiveAddrUse, 1)
	})
	if err != nil {
		return err
	}
	return serr
}
