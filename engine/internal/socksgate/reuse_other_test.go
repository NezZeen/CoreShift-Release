//go:build !windows

package socksgate

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// reuse is what a program trying to share a port in use sets: SO_REUSEPORT
// lets it in where the port's holder set it too.
func reuse(_, _ string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		if serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); serr == nil {
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}
	})
	if err != nil {
		return err
	}
	return serr
}
