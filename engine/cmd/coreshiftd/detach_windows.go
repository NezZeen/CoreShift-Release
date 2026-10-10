package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach makes cmd a process of its own: no console, its own group and,
// with breakaway, out of the app's job, so closing the app leaves it
// running. A job that allows no breakaway refuses the start with it.
func detach(cmd *exec.Cmd, breakaway bool) {
	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	if breakaway {
		flags |= windows.CREATE_BREAKAWAY_FROM_JOB
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
}
