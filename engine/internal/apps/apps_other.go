//go:build !windows && !linux

package apps

func running() ([]App, error) { return nil, nil }

func isSystem(string) bool { return false }
