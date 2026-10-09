package proc

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

// From iprtrmib.h and tcpmib.h.
const (
	tcpTableOwnerPIDListener = 3
	// The sizes of MIB_TCPROW_OWNER_PID and MIB_TCP6ROW_OWNER_PID.
	tcpRowSize  = 24
	tcp6RowSize = 56
)

// listens finds the owners of the listeners for addr in the system's TCP
// tables, IPv4 and IPv6.
func listens(pid int, addr netip.AddrPort) (bool, error) {
	found := false
	for _, af := range []uint32{windows.AF_INET, windows.AF_INET6} {
		b, err := listenerTable(af)
		if err != nil {
			return false, err
		}
		if len(b) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(b))
		row := tcpRowSize
		if af == windows.AF_INET6 {
			row = tcp6RowSize
		}
		for i := range n {
			off := 4 + i*row
			if off+row > len(b) {
				break
			}
			r := b[off : off+row]
			var local netip.AddrPort
			var owner uint32
			if af == windows.AF_INET {
				// dwState, dwLocalAddr, dwLocalPort, dwRemoteAddr, dwRemotePort, dwOwningPid;
				// addresses and ports in network order.
				local = netip.AddrPortFrom(netip.AddrFrom4([4]byte(r[4:8])), binary.BigEndian.Uint16(r[8:10]))
				owner = binary.LittleEndian.Uint32(r[20:24])
			} else {
				// ucLocalAddr[16], dwLocalScopeId, dwLocalPort, ucRemoteAddr[16],
				// dwRemoteScopeId, dwRemotePort, dwState, dwOwningPid.
				local = netip.AddrPortFrom(netip.AddrFrom16([16]byte(r[0:16])), binary.BigEndian.Uint16(r[20:22]))
				owner = binary.LittleEndian.Uint32(r[52:56])
			}
			if !matches(local, addr) {
				continue
			}
			if int(owner) != pid {
				return false, nil
			}
			found = true
		}
	}
	return found, nil
}

// listenerTable returns the MIB_TCP(6)TABLE_OWNER_PID of the listeners of
// address family af.
func listenerTable(af uint32) ([]byte, error) {
	size := uint32(16 << 10)
	for range 5 {
		b := make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), 0,
			uintptr(af), tcpTableOwnerPIDListener, 0)
		switch windows.Errno(r) {
		case 0:
			return b[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			size += 4 << 10 // the table may grow before the next call
		default:
			return nil, errors.Join(ErrOwnerUnknown, windows.Errno(r))
		}
	}
	return nil, ErrOwnerUnknown
}
