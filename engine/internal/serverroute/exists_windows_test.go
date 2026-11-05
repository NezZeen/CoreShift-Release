//go:build windows

package serverroute

import (
	"unsafe"
)

var procGetIpForwardEntry2 = iphlpapi.NewProc("GetIpForwardEntry2")

// routeExists reports whether the system's table holds r, looked up by the
// same row Add creates (interface, destination, next hop): a wrong offset in
// forwardRow would make Add succeed with a route that is not r.
func routeExists(r Route) bool {
	row := forwardRow(r)
	rc, _, _ := procGetIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row[0])))
	return rc == 0
}
