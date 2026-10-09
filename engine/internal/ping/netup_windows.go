package ping

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultRouteInterface reads the active route table of both families: when an
// adapter loses its link or its Wi-Fi, Windows takes its routes out of it.
func defaultRouteInterface(skip []string) (string, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_UNSPEC, &table); err != nil {
		return "", fmt.Errorf("GetIpForwardTable2: %w", err)
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	for _, r := range table.Rows() {
		if r.DestinationPrefix.PrefixLength != 0 || r.Loopback != 0 {
			continue
		}
		ifc, err := net.InterfaceByIndex(int(r.InterfaceIndex))
		if err == nil && usableInterface(ifc, skip) {
			return ifc.Name, nil
		}
	}
	return "", nil
}
