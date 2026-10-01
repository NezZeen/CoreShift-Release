package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// installRecoveryTask (re)creates the boot-time DNS recovery task for the
// service's executable. It needs administrator rights, as service install does.
func installRecoveryTask(exe string) error {
	dir, err := os.MkdirTemp("", "coreshift-task")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	xmlPath := filepath.Join(dir, "task.xml")
	if err := utf16File(xmlPath, bootRecoveryTaskXML(exe)); err != nil {
		return err
	}
	return schtasks("/Create", "/TN", recoveryTaskName, "/XML", xmlPath, "/F")
}

// removeRecoveryTask deletes the task; one that is not there is not an error.
func removeRecoveryTask() error {
	if err := schtasks("/Query", "/TN", recoveryTaskName); err != nil {
		return nil
	}
	return schtasks("/Delete", "/TN", recoveryTaskName, "/F")
}

func schtasks(args ...string) error {
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "schtasks.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}
