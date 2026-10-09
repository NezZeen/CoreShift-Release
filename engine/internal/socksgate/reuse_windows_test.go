package socksgate

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// reuse is what a program trying to share a port in use sets: on Windows
// SO_REUSEADDR lets it bind over a socket without SO_EXCLUSIVEADDRUSE.
func reuse(_, _ string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		serr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return serr
}
