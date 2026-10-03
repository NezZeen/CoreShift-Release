package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"coreshift/engine/internal/selfupdate"
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

// The staged copy starts while it is held open against changes.
func TestLaunchStagedInstaller(t *testing.T) {
	who, err := exec.LookPath("whoami.exe")
	if err != nil {
		t.Skip(err)
	}
	sum, err := selfupdate.FileSHA256(who)
	if err != nil {
		t.Fatal(err)
	}
	st, err := stageInstaller(who, sum, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := launchInstaller(st.path, filepath.Join(t.TempDir(), "install.log")); err != nil {
		t.Fatal(err)
	}
}
