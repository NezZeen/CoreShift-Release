package dnsguard

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	winreg "golang.org/x/sys/windows/registry"
)

var (
	dnsapi                    = windows.NewLazySystemDLL("dnsapi.dll")
	procDnsFlushResolverCache = dnsapi.NewProc("DnsFlushResolverCache")
	userenv                   = windows.NewLazySystemDLL("userenv.dll")
	procRefreshPolicyEx       = userenv.NewProc("RefreshPolicyEx")
)

// New returns the Windows guard. It must run as SYSTEM or an administrator.
func New(journalPath string) (Guard, error) {
	j, err := OpenJournal(journalPath)
	if err != nil {
		return nil, err
	}
	return &windowsGuard{
		reg:           hklm{},
		journal:       j,
		comment:       ruleCommentFor(journalPath),
		flushCache:    flushResolverCache,
		refreshPolicy: refreshMachinePolicy,
		newRuleID:     newGUID,
	}, nil
}

func flushResolverCache() error {
	if err := procDnsFlushResolverCache.Find(); err != nil {
		return err
	}
	if r, _, err := procDnsFlushResolverCache.Call(); r == 0 {
		return fmt.Errorf("DnsFlushResolverCache: %w", err)
	}
	return nil
}

// refreshMachinePolicy makes the DNS client re-read policy keys (NRPT rules
// under SOFTWARE\Policies and DisableSmartNameResolution).
func refreshMachinePolicy() error {
	const rpForce = 1
	if err := procRefreshPolicyEx.Find(); err != nil {
		return err
	}
	if r, _, err := procRefreshPolicyEx.Call(1, rpForce); r == 0 {
		return fmt.Errorf("RefreshPolicyEx: %w", err)
	}
	return nil
}

// Flags and constants from iphlpapi.h / ipifcons.h.
const (
	gaaSkipAnycast     = 0x2
	gaaSkipMulticast   = 0x4
	gaaIncludeGateways = 0x80
	ifTypeLoopback     = 24
	ifOperStatusUp     = 1
)

// SystemResolvers returns the DNS servers of connected adapters that have a
// default gateway, skipping the interface named exclude (our TUN). Those of
// the adapter that carries internet traffic come first: the adapters' own
// order says nothing about it, and virtual ones such as Radmin VPN have a
// gateway too. Call it before Apply: afterwards the system resolves
// through the tunnel.
func SystemResolvers(ctx context.Context, exclude string) ([]netip.Addr, error) {
	var first, rest []netip.Addr
	err := eachAdapter(exclude, func(aa *windows.IpAdapterAddresses, best bool) {
		to := &rest
		if best {
			to = &first
		}
		for d := aa.FirstDnsServerAddress; d != nil; d = d.Next {
			if a, ok := netip.AddrFromSlice(d.Address.IP()); ok {
				*to = append(*to, a.Unmap())
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return FilterUsable(append(first, rest...)), nil
}

// SearchDomains returns the network's own domains: the DNS suffix and
// search list of the adapters SystemResolvers reads, the internet one's
// first, then Windows' search list (set by hand or by a group policy).
func SearchDomains(ctx context.Context, exclude string) ([]string, error) {
	var first, rest []string
	err := eachAdapter(exclude, func(aa *windows.IpAdapterAddresses, best bool) {
		to := &rest
		if best {
			to = &first
		}
		if aa.DnsSuffix != nil {
			*to = append(*to, windows.UTF16PtrToString(aa.DnsSuffix))
		}
		for s := aa.FirstDnsSuffix; s != nil; s = s.Next {
			*to = append(*to, windows.UTF16ToString(s.String[:]))
		}
	})
	if err != nil {
		return nil, err
	}
	for _, at := range []struct{ path, value string }{
		{`SOFTWARE\Policies\Microsoft\Windows NT\DNSClient`, "SearchList"},
		{`SYSTEM\CurrentControlSet\Services\Tcpip\Parameters`, "SearchList"},
	} {
		k, err := winreg.OpenKey(winreg.LOCAL_MACHINE, at.path, winreg.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue(at.value)
		k.Close()
		if err == nil {
			rest = append(rest, strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })...)
		}
	}
	return CleanSearchDomains(append(first, rest...)), nil
}

// eachAdapter calls f for every connected adapter with a default gateway
// but the one named exclude; best tells the adapter that carries internet
// traffic.
func eachAdapter(exclude string, f func(aa *windows.IpAdapterAddresses, best bool)) error {
	size := uint32(16 << 10)
	var buf []byte
	for {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, gaaSkipAnycast|gaaSkipMulticast|gaaIncludeGateways,
			0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			return fmt.Errorf("dnsguard: GetAdaptersAddresses: %w", err)
		}
	}
	// With our TUN up, the best interface is the TUN, which is skipped: the
	// adapters then keep their order.
	var best uint32
	_ = windows.GetBestInterfaceEx(&windows.SockaddrInet4{Addr: [4]byte{1, 1, 1, 1}}, &best)
	for aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); aa != nil; aa = aa.Next {
		if aa.OperStatus != ifOperStatusUp || aa.IfType == ifTypeLoopback || aa.FirstGatewayAddress == nil {
			continue
		}
		if exclude != "" && strings.EqualFold(windows.UTF16PtrToString(aa.FriendlyName), exclude) {
			continue
		}
		f(aa, best != 0 && aa.IfIndex == best)
	}
	return nil
}
