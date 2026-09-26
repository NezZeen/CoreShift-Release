//go:build !windows

package core

import "os/exec"

func hideWindow(*exec.Cmd) {}
