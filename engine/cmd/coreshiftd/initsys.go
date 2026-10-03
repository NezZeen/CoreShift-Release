package main

import "fmt"

// The Linux service runs under whichever init system the distribution has:
// systemd (most), OpenRC (Alpine, Gentoo, Artix) or runit (Void, Artix).
// packaging/linux installs a unit, an init script or a run script for it;
// "coreshiftd service start|stop|status" asks the one that is running.

type initSystem string

const (
	initSystemd initSystem = "systemd"
	initOpenRC  initSystem = "openrc"
	initRunit   initSystem = "runit"
	initUnknown initSystem = ""
)

// detectInit names the running init system from what is on disk, as
// exists reports it: each leaves its mark under /run while it runs.
func detectInit(exists func(path string) bool) initSystem {
	switch {
	case exists("/run/systemd/system"):
		return initSystemd
	case exists("/run/openrc"):
		return initOpenRC
	case exists("/run/runit") || exists("/etc/runit/runsvdir") || exists("/var/service") && exists("/usr/bin/sv"):
		return initRunit
	}
	return initUnknown
}

// serviceCommand is the command that does action (start, stop, status) to
// the CoreShift service under init.
func serviceCommand(init initSystem, action string) ([]string, error) {
	switch action {
	case "start", "stop", "status":
	default:
		return nil, fmt.Errorf("unknown service action %q", action)
	}
	switch init {
	case initSystemd:
		return []string{"systemctl", action, "coreshift.service"}, nil
	case initOpenRC:
		return []string{"rc-service", "coreshift", action}, nil
	case initRunit:
		return []string{"sv", action, "coreshift"}, nil
	}
	return nil, fmt.Errorf("no systemd, OpenRC or runit here: run %q as root from your init system; see packaging/linux/README.md",
		"coreshiftd service run")
}
