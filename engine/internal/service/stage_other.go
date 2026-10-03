//go:build !windows

package service

import "os"

// openLocked opens path for reading. Elsewhere files cannot be locked
// against renaming; the folder they are in is the service's alone.
func openLocked(path string) (*os.File, error) { return os.Open(path) }
