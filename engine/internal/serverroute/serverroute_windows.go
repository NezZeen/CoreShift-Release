//go:build windows

package serverroute

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi                     = windows.NewLazySystemDLL("iphlpapi.dll")
	procInitializeIpForwardEntry = iphlpapi.NewProc("InitializeIpForwardEntry")
	procCreateIpForwardEntry2    = iphlpapi.NewProc("CreateIpForwardEntry2")
	procDeleteIpForwardEntry2    = iphlpapi.NewProc("DeleteIpForwardEntry2")
)

// Route table error codes worth naming.
const (
	errObjectAlreadyExists = syscall.Errno(5010) // ERROR_OBJECT_ALREADY_EXISTS
	errNotFound            = syscall.Errno(1168) // ERROR_NOT_FOUND
)

// MIB_IPFORWARD_ROW2 is 104 bytes on x64 and x86 alike (4-byte alignment):
//
//	offset 0:   InterfaceLuid (8 bytes, left 0: InterfaceIndex selects)
//	offset 8:   InterfaceIndex uint32
//	offset 12:  DestinationPrefix.Prefix SOCKADDR_INET (28 bytes)
//	offset 40:  DestinationPrefix.PrefixLength uint8 (+3 pad)
//	offset 44:  NextHop SOCKADDR_INET (28 bytes)
//	offset 72:  SitePrefixLength uint8 (+3 pad)
//	offset 76:  ValidLifetime uint32
//	offset 80:  PreferredLifetime uint32
//	offset 84:  Metric uint32
//	offset 88:  Protocol uint32
//	offset 92:  Loopback, AutoconfigureAddress, Publish, Immortal (4 bools)
//	offset 96:  Age uint32
//	offset 100: Origin uint32
const mibIPForwardRow2Size = 104

const (
	afInet            = 2
	mibIPProtoNetmgmt = 3
)

// forwardRow packs r as a MIB_IPFORWARD_ROW2 for Create/DeleteIpForwardEntry2.
func forwardRow(r Route) []byte {
	b := make([]byte, mibIPForwardRow2Size)
	procInitializeIpForwardEntry.Call(uintptr(unsafe.Pointer(&b[0])))
	binary.LittleEndian.PutUint32(b[8:], uint32(r.IfIndex))
	putSockaddrInet(b[12:40], r.IP)
	b[40] = 32
	putSockaddrInet(b[44:72], r.Gateway)
	binary.LittleEndian.PutUint32(b[76:], 0xFFFFFFFF) // ValidLifetime: forever
	binary.LittleEndian.PutUint32(b[80:], 0xFFFFFFFF) // PreferredLifetime
	binary.LittleEndian.PutUint32(b[84:], 1)          // Metric
	binary.LittleEndian.PutUint32(b[88:], mibIPProtoNetmgmt)
	return b
}

// putSockaddrInet writes a v4 SOCKADDR_INET: family, port 0, address in
// network byte order at offset 4.
func putSockaddrInet(b []byte, a netip.Addr) {
	binary.LittleEndian.PutUint16(b, afInet)
	a4 := a.As4()
	copy(b[4:8], a4[:])
}

func addRoute(r Route) error {
	row := forwardRow(r)
	rc, _, _ := procCreateIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row[0])))
	if errno := syscall.Errno(rc); errno != 0 {
		if errno != errObjectAlreadyExists {
			return fmt.Errorf("CreateIpForwardEntry2: %w", errno)
		}
		// A leftover route to the same server: replace it.
		procDeleteIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row[0])))
		if rc, _, _ := procCreateIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row[0]))); rc != 0 {
			return fmt.Errorf("CreateIpForwardEntry2: %w", syscall.Errno(rc))
		}
	}
	return nil
}

func delRoute(r Route) error {
	row := forwardRow(r)
	rc, _, _ := procDeleteIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row[0])))
	if errno := syscall.Errno(rc); errno != 0 && errno != errNotFound {
		return fmt.Errorf("DeleteIpForwardEntry2: %w", errno)
	}
	return nil
}
