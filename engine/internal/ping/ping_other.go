//go:build !windows && !linux

package ping

import (
	"context"
	"net/netip"
	"syscall"
	"time"
)

func echo(context.Context, netip.Addr, Bind, uint16, time.Duration) (time.Duration, error) {
	return 0, ErrUnsupported
}

func control(Bind) func(network, address string, c syscall.RawConn) error { return nil }

func defaultRoutes() ([]route, error) { return nil, ErrUnsupported }
