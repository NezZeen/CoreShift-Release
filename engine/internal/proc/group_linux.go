package proc

import (
	"os"
	"os/exec"
	"syscall"
)

// processGroup is a no-op on Linux and Android: Pdeathsig (set in
// prepareCmd) already kills cores when the daemon dies.
type processGroup struct{}

func newProcessGroup() (*processGroup, error) { return &processGroup{}, nil }

func (*processGroup) add(*os.Process) error { return nil }

// prepareCmd asks the kernel to kill the core if the daemon dies. Pdeathsig
// fires when the starting OS thread exits; the Go runtime keeps threads alive
// for the process lifetime unless LockOSThread is misused.
func prepareCmd(cmd *exec.Cmd, _ bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

func interrupt(p *os.Process) error { return p.Signal(syscall.SIGTERM) }

// resume has nothing to do: processes start running here.
func resume(*os.Process) error { return nil }
