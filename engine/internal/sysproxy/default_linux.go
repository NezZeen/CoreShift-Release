package sysproxy

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// Default is the manager for the user running this: the desktop's
// backends, the journal in ~/.local/state/coreshift, the autostart entry.
func Default(ctx context.Context) (*Manager, error) {
	if os.Getenv("HOME") == "" {
		return nil, errors.New("HOME is not set")
	}
	has := func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	}
	journal, autostart := linuxPaths(os.Getenv)
	m := &Manager{Journal: journal, Backends: Detect(ctx, os.Getenv, Exec, has)}
	if self, err := os.Executable(); err == nil {
		m.Autostart = XDGAutostart{Path: autostart, Exec: quoteExec(self) + " sysproxy logon"}
	}
	return m, nil
}
