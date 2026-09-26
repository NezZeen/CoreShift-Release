package ping

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The ICMP helper API needs no administrator rights, unlike raw sockets.
var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho2Ex = iphlpapi.NewProc("IcmpSendEcho2Ex")
)

// icmpEchoReply is ICMP_ECHO_REPLY; Go lays it out as C does.
type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	TTL           uint8
	TOS           uint8
	Flags         uint8
	OptionsSize   uint8
	OptionsData   uintptr
}

// IP_STATUS codes worth naming.
var ipStatus = map[uint32]string{
	11002: "destination network unreachable",
	11003: "destination host unreachable",
	11004: "destination protocol unreachable",
	11005: "destination port unreachable",
	11010: "request timed out",
	11013: "TTL expired in transit",
	11050: "general failure",
}

func statusError(code uint32) error {
	if s, ok := ipStatus[code]; ok {
		return errors.New(s)
	}
	return fmt.Errorf("status %d", code)
}

func echo(_ context.Context, dst netip.Addr, b Bind, seq uint16, timeout time.Duration) (time.Duration, error) {
	if !dst.Is4() {
		return 0, ErrUnsupported
	}
	if err := procIcmpSendEcho2Ex.Find(); err != nil {
		return 0, ErrUnsupported
	}
	h, _, err := procIcmpCreateFile.Call()
	if windows.Handle(h) == windows.InvalidHandle {
		return 0, fmt.Errorf("IcmpCreateFile: %w", err)
	}
	defer procIcmpCloseHandle.Call(h)

	var src uint32
	if b.Source.Is4() {
		s := b.Source.As4()
		src = binary.LittleEndian.Uint32(s[:])
	}
	d := dst.As4()
	payload := []byte("coreshift-ping-" + fmt.Sprint(seq))
	reply := make([]byte, 512)
	start := time.Now()
	n, _, err := procIcmpSendEcho2Ex.Call(h, 0, 0, 0,
		uintptr(src), uintptr(binary.LittleEndian.Uint32(d[:])),
		uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout.Milliseconds()))
	elapsed := time.Since(start)
	if n == 0 {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno != 0 {
			if _, ok := ipStatus[uint32(errno)]; ok {
				return 0, statusError(uint32(errno))
			}
			return 0, errno
		}
		return 0, statusError(11050)
	}
	r := (*icmpEchoReply)(unsafe.Pointer(&reply[0]))
	if r.Status != 0 {
		return 0, statusError(r.Status)
	}
	// RoundTripTime has millisecond resolution; the wall clock is finer.
	rtt := time.Duration(r.RoundTripTime) * time.Millisecond
	if elapsed < rtt+time.Millisecond {
		rtt = elapsed
	}
	return max(rtt, time.Microsecond), nil
}

func control(Bind) func(network, address string, c syscall.RawConn) error { return nil }

var procGetIpForwardTable = iphlpapi.NewProc("GetIpForwardTable")

// mibIPForwardRow is MIB_IPFORWARDROW: addresses in network byte order.
type mibIPForwardRow struct {
	Dest, Mask, Policy, NextHop, IfIndex, Type, Proto, Age, NextHopAS uint32
	Metric1, Metric2, Metric3, Metric4, Metric5                       uint32
}

// defaultRoutes lists the IPv4 routes to 0.0.0.0/0 with their interface
// index and metric. VPN clients usually add narrower routes (0.0.0.0/1 and
// the like) on top, which these leave out.
func defaultRoutes() ([]route, error) {
	var size uint32
	procGetIpForwardTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size == 0 {
		return nil, errors.New("empty route table")
	}
	buf := make([]byte, size)
	r, _, _ := procGetIpForwardTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 1)
	if r != 0 {
		return nil, fmt.Errorf("GetIpForwardTable: %w", syscall.Errno(r))
	}
	n := binary.LittleEndian.Uint32(buf)
	rows := unsafe.Slice((*mibIPForwardRow)(unsafe.Pointer(&buf[4])), n)
	var out []route
	for _, row := range rows {
		if row.Dest == 0 && row.Mask == 0 {
			var gw [4]byte
			binary.LittleEndian.PutUint32(gw[:], row.NextHop)
			out = append(out, route{ifIndex: int(row.IfIndex), metric: int(row.Metric1), gateway: netip.AddrFrom4(gw)})
		}
	}
	return out, nil
}
