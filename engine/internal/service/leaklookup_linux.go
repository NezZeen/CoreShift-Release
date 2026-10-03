package service

import (
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"slices"
	"strings"
)

// systemLookup resolves host as other programs do: through glibc and
// whatever /etc/resolv.conf or systemd-resolved says, which the DNS guard
// points at the tunnel. It runs getent rather than resolving in-process:
// the TUN layer answers the daemon's own lookups with the direct resolver
// (tunlayer.Options.DirectDNSProcesses), which is not what apps get.
func systemLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	path, err := exec.LookPath("getent")
	if err != nil {
		return nil, errors.New("getent not found: cannot look names up as apps do")
	}
	out, err := exec.CommandContext(ctx, path, "ahosts", host).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 2 {
			return nil, errors.New("no such name")
		}
		return nil, err
	}
	return parseGetent(string(out)), nil
}

// parseGetent reads `getent ahosts`: an address first on each line.
func parseGetent(out string) []netip.Addr {
	var addrs []netip.Addr
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if a, err := netip.ParseAddr(f[0]); err == nil && !slices.Contains(addrs, a.Unmap()) {
			addrs = append(addrs, a.Unmap())
		}
	}
	return addrs
}
