package ping

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// echo uses a raw ICMP socket, which needs root or CAP_NET_RAW; the daemon
// has them, and without them the caller falls back to TCP.
func echo(ctx context.Context, dst netip.Addr, b Bind, seq uint16, timeout time.Duration) (time.Duration, error) {
	network, reqType, replyType := "ip4:icmp", byte(8), byte(0)
	if dst.Is6() {
		network, reqType, replyType = "ip6:ipv6-icmp", 128, 129
	}
	laddr := ""
	if b.Source.IsValid() && b.Source.Is6() == dst.Is6() {
		laddr = b.Source.String()
	}
	lc := net.ListenConfig{Control: control(b)}
	c, err := lc.ListenPacket(ctx, network, laddr)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return 0, ErrUnsupported
		}
		return 0, err
	}
	defer c.Close()

	id := uint16(os.Getpid())
	msg := []byte{reqType, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(msg[4:], id)
	binary.BigEndian.PutUint16(msg[6:], seq)
	msg = append(msg, "coreshift-ping"...)
	if !dst.Is6() { // the kernel fills in the ICMPv6 checksum
		binary.BigEndian.PutUint16(msg[2:], checksum(msg))
	}

	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	start := time.Now()
	if _, err := c.WriteTo(msg, &net.IPAddr{IP: dst.AsSlice()}); err != nil {
		return 0, err
	}
	buf := make([]byte, 1500)
	for {
		n, from, err := c.ReadFrom(buf)
		if err != nil {
			return 0, err
		}
		ip, ok := from.(*net.IPAddr)
		if !ok || n < 8 || buf[0] != replyType {
			continue
		}
		a, _ := netip.AddrFromSlice(ip.IP)
		// Raw sockets see every reply on the host, so match ours exactly.
		if a.Unmap() == dst && binary.BigEndian.Uint16(buf[4:]) == id && binary.BigEndian.Uint16(buf[6:]) == seq {
			return max(time.Since(start), time.Microsecond), nil
		}
	}
}

func checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// control binds sockets to b.Interface, so they leave through it even while
// a tunnel holds the default route.
func control(b Bind) func(network, address string, c syscall.RawConn) error {
	if b.Interface == "" {
		return nil
	}
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = syscall.BindToDevice(int(fd), b.Interface)
		})
		if err != nil {
			return err
		}
		return serr
	}
}

// defaultRoutes reads the IPv4 default routes from /proc/net/route.
func defaultRoutes() ([]route, error) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil, err
	}
	var out []route
	for _, line := range strings.Split(string(data), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 8 || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		ifc, err := net.InterfaceByName(f[0])
		if err != nil {
			continue
		}
		metric, _ := strconv.Atoi(f[6])
		var gw [4]byte
		if v, err := strconv.ParseUint(f[2], 16, 32); err == nil {
			binary.LittleEndian.PutUint32(gw[:], uint32(v))
		}
		out = append(out, route{ifIndex: ifc.Index, metric: metric, gateway: netip.AddrFrom4(gw)})
	}
	return out, nil
}
