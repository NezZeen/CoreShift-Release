//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach makes cmd a session of its own, so closing the app leaves it
// running.
func detach(cmd *exec.Cmd, _ bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
