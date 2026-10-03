//go:build !windows && (!linux || android)

package service

import "os"

func restrictDir(dir string) error { return os.Chmod(dir, 0o700) }

// WriteShared writes a file the unprivileged UI must be able to read.
func WriteShared(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}

// SecureDataDir only creates root here (Android, where the directory is
// the app's own); Linux has its own in secure_linux.go.
func SecureDataDir(root string) ([]string, error) { return nil, os.MkdirAll(root, 0o700) }

func elevated() bool { return os.Geteuid() == 0 }
