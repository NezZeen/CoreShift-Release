// Package ping measures the network delay to a server without a proxy: an
// ICMP echo, or the time to open a TCP connection where ICMP is blocked.
package ping

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"time"
)

// ErrUnsupported means ICMP echo is not available here, e.g. for lack of
// privileges or for this address family.
var ErrUnsupported = errors.New("ICMP echo is not supported here")

// Bind makes probes leave through a particular network interface, e.g. the
// physical one while a VPN tunnel holds the default route. The zero value
// uses the system's routing.
type Bind struct {
	// Source is the local address probes are sent from. On Windows, which
	// uses the strong host model, it also selects the interface.
	Source netip.Addr
	// Interface is the name of the interface to bind to (Linux).
	Interface string
}

// ICMP sends up to count echo requests to dst, each waiting up to timeout,
// and returns the fastest round trip. It fails only if every request did.
func ICMP(ctx context.Context, dst netip.Addr, b Bind, count int, timeout time.Duration) (time.Duration, error) {
	best, err := time.Duration(0), error(nil)
	for i := range count {
		if ctx.Err() != nil {
			break
		}
		rtt, e := echo(ctx, dst.Unmap(), b, uint16(i+1), timeout)
		if e != nil {
			if errors.Is(e, ErrUnsupported) {
				return 0, e
			}
			err = e
			continue
		}
		if best == 0 || rtt < best {
			best = rtt
		}
	}
	if best > 0 {
		return best, nil
	}
	if err == nil {
		err = ctx.Err()
	}
	return 0, err
}

// TCP opens up to count connections to dst, each waiting up to timeout, and
// returns the fastest handshake. It fails only if every attempt did. A
// first attempt that fails is tried once more, as a lost SYN or a mobile
// radio waking up costs one; a second failure ends it, as a server that does
// not answer twice seldom answers a third time, and waiting for each would
// make a list of servers slow to test. A server that does not answer at all
// costs at most two timeouts.
func TCP(ctx context.Context, dst netip.AddrPort, b Bind, count int, timeout time.Duration) (time.Duration, error) {
	return tcp(ctx, dst, count, timeout, b.Dialer(timeout).DialContext)
}

// tcpRetryPause is the pause before trying a failed first handshake again.
var tcpRetryPause = 200 * time.Millisecond

// tcp is TCP with the dialer given, for tests.
func tcp(ctx context.Context, dst netip.AddrPort, count int, timeout time.Duration,
	dial func(ctx context.Context, network, addr string) (net.Conn, error)) (time.Duration, error) {
	best, err := time.Duration(0), error(nil)
	retried := false
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			break
		}
		actx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		c, e := dial(actx, "tcp", dst.String())
		cancel()
		if e != nil {
			err = e
			if best > 0 {
				continue
			}
			if retried {
				break
			}
			retried = true
			i-- // the retry does not count as an attempt of its own
			select {
			case <-ctx.Done():
			case <-time.After(tcpRetryPause):
			}
			continue
		}
		rtt := max(time.Since(start), time.Microsecond) // the clock may not tick on loopback
		c.Close()
		if best == 0 || rtt < best {
			best = rtt
		}
	}
	if best > 0 {
		return best, nil
	}
	if err == nil {
		err = ctx.Err()
	}
	return 0, err
}

// Dialer makes TCP connections that leave as b says: around the tunnel,
// through the physical interface.
func (b Bind) Dialer(timeout time.Duration) *net.Dialer {
	d := &net.Dialer{Timeout: timeout, Control: control(b)}
	if b.Source.IsValid() {
		d.LocalAddr = &net.TCPAddr{IP: b.Source.AsSlice()}
	}
	return d
}

type route struct {
	ifIndex int
	metric  int
	gateway netip.Addr
}

// Physical returns where probes leave the computer when no VPN is in the
// way: the IPv4 address of the interface holding the preferred default route
// (0.0.0.0/0). Tunnels usually claim traffic with narrower routes, so this
// skips them without knowing about them; skip names interfaces to ignore
// anyway, such as the engine's own TUN.
func Physical(skip ...string) (Bind, error) {
	routes, err := defaultRoutes()
	if err != nil {
		return Bind{}, err
	}
	slices.SortStableFunc(routes, func(a, b route) int { return a.metric - b.metric })
	for _, r := range routes {
		ifc, err := net.InterfaceByIndex(r.ifIndex)
		if err != nil || ifc.Flags&net.FlagUp == 0 || slices.Contains(skip, ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		var first netip.Addr
		for _, a := range addrs {
			p, err := netip.ParsePrefix(a.String())
			if err != nil || !p.Addr().Is4() {
				continue
			}
			// Prefer the address on the gateway's subnet.
			if p.Contains(r.gateway) {
				return Bind{Source: p.Addr(), Interface: ifc.Name}, nil
			}
			if !first.IsValid() {
				first = p.Addr()
			}
		}
		if first.IsValid() {
			return Bind{Source: first, Interface: ifc.Name}, nil
		}
	}
	return Bind{}, errors.New("no default route")
}

func (b Bind) String() string {
	if b.Interface != "" {
		return fmt.Sprintf("%s (%s)", b.Source, b.Interface)
	}
	return b.Source.String()
}
