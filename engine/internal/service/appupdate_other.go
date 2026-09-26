//go:build !windows

package service

import "errors"

// Self-update installs a Windows installer; elsewhere it stays off.

func launchInstaller(path, logPath string) error {
	return errors.New("self-update is only for Windows")
}

func appSessions() []uint32 { return nil }

func startApp(sessions []uint32) error { return nil }
