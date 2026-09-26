//go:build !windows && !linux

package proc

import (
	"os"
	"os/exec"
)

type processGroup struct{}

func newProcessGroup() (*processGroup, error) { return &processGroup{}, nil }

func (*processGroup) add(*os.Process) error { return nil }

func prepareCmd(*exec.Cmd, bool) {}

func interrupt(p *os.Process) error { return p.Signal(os.Interrupt) }
