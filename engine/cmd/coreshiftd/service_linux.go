//go:build linux && !android

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"coreshift/engine/internal/service"
)

// serviceUnit is the systemd unit the packages install
// (packaging/linux/coreshift.service).
const serviceUnit = "coreshift.service"

// isElevated reports whether the daemon may create the TUN interface:
// root, or a process given CAP_NET_ADMIN (systemd's AmbientCapabilities).
func isElevated() bool {
	if os.Geteuid() == 0 {
		return true
	}
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	caps, ok := effectiveCaps(b)
	return ok && hasCap(caps, capNetAdmin)
}

// runService is the Linux service. systemd runs "service run" as root from
// boot (packaging/linux/coreshift.service); the package installs and
// enables it, so install and uninstall only say so. start and stop ask
// systemd.
func runService(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: coreshiftd service {run|start|stop|status} [flags]")
	}
	switch args[0] {
	case "run":
		return runLinuxService(ctx, args[1:])
	case "start", "stop", "status":
		cmd := exec.CommandContext(ctx, "systemctl", args[0], serviceUnit)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	case "install", "uninstall":
		return fmt.Errorf("on Linux the %s package installs %s and enables it; see packaging/linux/README.md", "coreshift", serviceUnit)
	}
	return fmt.Errorf("unknown service command %q", args[0])
}

// selfUpdateOff is why the Linux daemon does not update CoreShift: its
// package manager does, see packaging/linux/README.md.
const selfUpdateOff = "linux: CoreShift is updated with its package"

func runLinuxService(ctx context.Context, args []string) error {
	// Everything the daemon creates is root's alone unless it says
	// otherwise (api.json, resolv.conf, core executables).
	syscall.Umask(0o077)

	fs := flag.NewFlagSet("service run", flag.ContinueOnError)
	df := addDaemonFlags(fs)
	apiAddr := fs.String("api", "127.0.0.1:17900", "loopback address of the UI API")
	follow := fs.Bool("follow-app", true, "connect when the app comes (with auto-connect on) and disconnect once it has been closed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := df.config()
	if err != nil {
		return err
	}
	cfg.SelfUpdate = false
	cfg.SelfUpdateOff = selfUpdateOff
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	// systemd keeps stdout in the journal too (journalctl -u coreshift),
	// but the file outlives a reboot on systems with a volatile journal.
	logPath := filepath.Join(cfg.DataDir, "coreshiftd.log")
	rotateLog(logPath, 3)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	log := io.MultiWriter(f, os.Stdout)
	fmt.Fprintf(log, "coreshiftd %s as uid %d, %s\n", service.VersionString(), os.Geteuid(), strings.Join(os.Args[1:], " "))
	err = serveWith(ctx, cfg, *apiAddr, log, serveOptions{followApp: *follow})
	if err != nil {
		fmt.Fprintln(log, "error:", err)
	}
	return err
}
