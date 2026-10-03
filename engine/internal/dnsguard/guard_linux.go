//go:build linux && !android

package dnsguard

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
)

const (
	resolvConf         = "/etc/resolv.conf"
	resolvedUpstream   = "/run/systemd/resolve/resolv.conf"
	resolvedVarlinkAPI = "/run/systemd/resolve/io.systemd.Resolve"
)

// New returns the Linux guard. It needs CAP_NET_ADMIN (resolvectl) or write
// access to /etc/resolv.conf.
func New(journalPath string) (Guard, error) {
	j, err := OpenJournal(journalPath)
	if err != nil {
		return nil, err
	}
	return &linuxGuard{
		journal:        j,
		run:            execCommand,
		resolvConfPath: resolvConf,
		useResolved:    resolvedInUse,
		networkManager: networkManagerRuns,
		linkExists: func(name string) bool {
			_, err := net.InterfaceByName(name)
			return err == nil
		},
		linkDirs: trustedLinkDirs,
	}, nil
}

// networkManagerRuns reports whether NetworkManager is up and nmcli can
// talk to it.
func networkManagerRuns() bool {
	if _, err := exec.LookPath("nmcli"); err != nil {
		return false
	}
	_, err := os.Stat("/run/NetworkManager")
	return err == nil
}

func execCommand(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// resolvedInUse reports whether resolved is running and glibc actually asks it,
// i.e. /etc/resolv.conf points at its stub listener.
func resolvedInUse() bool {
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return false
	}
	if _, err := os.Stat(resolvedVarlinkAPI); err != nil {
		return false
	}
	b, err := os.ReadFile(resolvConf)
	return err == nil && usesResolvedStub(b)
}

// SystemResolvers returns the upstream resolvers the system currently uses.
// Call it before Apply: afterwards the system resolves through the tunnel.
func SystemResolvers(ctx context.Context, exclude string) ([]netip.Addr, error) {
	path := resolvConf
	if resolvedInUse() {
		path = resolvedUpstream
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dnsguard: %w", err)
	}
	return filterUsable(resolvConfNameservers(b)), nil
}
