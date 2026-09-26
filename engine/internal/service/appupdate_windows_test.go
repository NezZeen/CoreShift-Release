package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessPath(t *testing.T) {
	self, _ := os.Executable()
	if p, ok := processPath(uint32(os.Getpid())); !ok || !strings.EqualFold(p, self) {
		t.Errorf("processPath = %q, %v; want %q", p, ok, self)
	}
}

// The installer is started detached; whoami stands in for it, rejecting the
// installer's switches and exiting at once.
func TestLaunchInstallerDetached(t *testing.T) {
	who, err := exec.LookPath("whoami.exe")
	if err != nil {
		t.Skip(err)
	}
	if err := launchInstaller(who, filepath.Join(t.TempDir(), "install.log")); err != nil {
		t.Fatal(err)
	}
}
