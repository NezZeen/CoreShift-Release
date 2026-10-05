package service

import "golang.org/x/sys/windows/registry"

// disabledNonTunnel is the DisabledComponents bit that switches IPv6 off on
// every interface but loopback and tunnel ones (ISATAP, 6to4, Teredo). A
// Wintun adapter is not a tunnel interface in Windows' terms, so with it
// the TUN layer cannot set its IPv6 address. 0xFF, the value guides give
// for "switch IPv6 off", includes it.
const disabledNonTunnel = 0x10

// ipv6Disabled reports whether Windows refuses IPv6 on new interfaces:
// HKLM\SYSTEM\CurrentControlSet\Services\Tcpip6\Parameters\DisabledComponents
// has disabledNonTunnel. Unchecking IPv6 on one adapter does not count: a
// new adapter gets it still.
func ipv6Disabled() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\Tcpip6\Parameters`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("DisabledComponents")
	return err == nil && ipv6DisabledBy(v)
}

func ipv6DisabledBy(components uint64) bool { return components&disabledNonTunnel != 0 }
