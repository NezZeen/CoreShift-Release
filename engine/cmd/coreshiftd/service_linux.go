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

// runService is the Linux service. The init system runs "service run" as
// root from boot (packaging/linux: a systemd unit, an OpenRC script or a
// runit service); the packages install and enable it, so install and
// uninstall only say so. start, stop and status ask the init system.
func runService(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: coreshiftd service {run|start|stop|status} [flags]")
	}
	switch args[0] {
	case "run":
		return runLinuxService(ctx, args[1:])
	case "start", "stop", "status":
		argv, err := serviceCommand(detectInit(pathExists), args[0])
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	case "install", "uninstall":
		return errors.New("on Linux the coreshift package (or install.sh) installs the service and enables it; see packaging/linux/README.md")
	}
	return fmt.Errorf("unknown service command %q", args[0])
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// selfUpdateOff is why the Linux daemon does not update CoreShift: its
// package manager does, see packaging/linux/README.md. New versions are
// still announced (Config.AnnounceUpdates).
const selfUpdateOff = "linux: CoreShift is updated with its package"

func runLinuxService(ctx context.Context, args []string) error {
	// Everything the daemon creates is root's alone unless it says
	// otherwise (api.json, resolv.conf, core executables).
	syscall.Umask(0o077)

	fs := flag.NewFlagSet("service run", flag.ContinueOnError)
	df := addDaemonFlags(fs)
	apiAddr := fs.String("api", "127.0.0.1:17900", "loopback address of the UI API")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := df.config()
	if err != nil {
		return err
	}
	cfg.SelfUpdate = false
	cfg.SelfUpdateOff = selfUpdateOff
	cfg.AnnounceUpdates = service.Version != "dev"
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	// The init system keeps stdout too (journalctl -u coreshift, OpenRC's
	// output_log, runit's svlogd), but the file outlives a reboot on
	// systems with a volatile log.
	logPath := filepath.Join(cfg.DataDir, "coreshiftd.log")
	rotateLog(logPath, 3)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	log := io.MultiWriter(f, os.Stdout)
	fmt.Fprintf(log, "coreshiftd %s as uid %d, %s\n", service.VersionString(), os.Geteuid(), strings.Join(os.Args[1:], " "))
	err = serveWith(ctx, cfg, *apiAddr, log, serveOptions{connectWithApp: true})
	if err != nil {
		fmt.Fprintln(log, "error:", err)
	}
	return err
}
