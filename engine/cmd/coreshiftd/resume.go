package main

import (
	"bytes"
	"os"
	"time"
)

// A restart of the Linux service must not turn the VPN off: the package
// manager restarts it on every upgrade (packaging/linux), and the VPN
// stays on until the user turns it off. Stopping while connected leaves a
// resume file naming the boot; the next start in the same boot, soon
// after, connects again. A reboot, a crash (no file is written) or a
// package removal (coreshift-setup.sh stop deletes it) does not.

// resumeMaxAge bounds how long after the stop a start still resumes.
const resumeMaxAge = 10 * time.Minute

// bootIDPath names the current boot; it changes with every boot.
const bootIDPath = "/proc/sys/kernel/random/boot_id"

func bootID() []byte {
	b, _ := os.ReadFile(bootIDPath)
	return bytes.TrimSpace(b)
}

// writeResume records that the VPN was on when the service stopped.
func writeResume(path string, boot []byte) error {
	if len(boot) == 0 {
		return nil // no way to tell a restart from a reboot
	}
	return os.WriteFile(path, append(boot, '\n'), 0o600)
}

// takeResume reports whether a resume file left in this boot, at most
// resumeMaxAge ago, asks to connect again. The file is used once.
func takeResume(path string, boot []byte, now time.Time) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	fi, statErr := os.Stat(path)
	os.Remove(path)
	if statErr != nil || len(boot) == 0 || !bytes.Equal(bytes.TrimSpace(b), boot) {
		return false
	}
	age := now.Sub(fi.ModTime())
	return age >= -time.Minute && age <= resumeMaxAge
}
