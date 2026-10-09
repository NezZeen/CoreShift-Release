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
	"path/filepath"
)

const (
	resolvConf         = "/etc/resolv.conf"
	resolvedUpstream   = "/run/systemd/resolve/resolv.conf"
	resolvedVarlinkAPI = "/run/systemd/resolve/io.systemd.Resolve"
)

// New returns the Linux guard. It needs root (or CAP_NET_ADMIN for
// resolvectl and write access to /etc/resolv.conf).
func New(journalPath string) (Guard, error) {
	j, err := OpenJournal(journalPath)
	if err != nil {
		return nil, err
	}
	return &linuxGuard{
		journal:        j,
		run:            execCommand,
		runInput:       execInput,
		resolvConfPath: resolvConf,
		detect:         func() dnsStack { return detectDNSStack(currentDNSEnv()) },
		networkManager: networkManagerRuns,
		firewalld:      firewalldRuns,
		selinux:        selinuxEnabled,
		linkExists: func(name string) bool {
			_, err := net.InterfaceByName(name)
			return err == nil
		},
		linkDirs: trustedLinkDirs,
	}, nil
}

func have(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// currentDNSEnv gathers what detectDNSStack decides on.
func currentDNSEnv() linuxDNSEnv {
	env := linuxDNSEnv{
		ResolvedRunning: have("resolvectl") && exists(resolvedVarlinkAPI),
		Resolvconf:      realResolvconf(),
		Netconfig:       have("netconfig"),
	}
	env.ResolvConf, _ = os.ReadFile(resolvConf)
	env.Link, _ = os.Readlink(resolvConf)
	return env
}

// realResolvconf reports whether resolvconf is installed and is not
// systemd-resolved's resolvectl under that name.
func realResolvconf() bool {
	p, err := exec.LookPath("resolvconf")
	if err != nil {
		return false
	}
	if target, err := filepath.EvalSymlinks(p); err == nil && filepath.Base(target) == "resolvectl" {
		return false
	}
	return true
}

// networkManagerRuns reports whether NetworkManager is up and nmcli can
// talk to it.
func networkManagerRuns() bool { return have("nmcli") && exists("/run/NetworkManager") }

// firewalldRuns reports whether firewalld is up: it leaves its PID file
// in /run while it runs.
func firewalldRuns() bool { return have("firewall-cmd") && exists("/run/firewalld") }

// selinuxEnabled reports whether SELinux runs (selinuxfs is mounted) and
// restorecon is there to relabel files.
func selinuxEnabled() bool { return have("restorecon") && exists("/sys/fs/selinux/enforce") }

func execCommand(ctx context.Context, name string, args ...string) error {
	return execInput(ctx, nil, name, args...)
}

func execInput(ctx context.Context, input []byte, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// resolvedInUse reports whether resolved is running and glibc actually asks it,
// i.e. /etc/resolv.conf points at its stub listener.
func resolvedInUse() bool { return detectDNSStack(currentDNSEnv()) == stackResolved }

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
	return FilterUsable(resolvConfNameservers(b)), nil
}
