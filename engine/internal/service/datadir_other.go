//go:build !linux || android

package service

import "os"

// prepareDataDir creates the data directory. On Windows its access comes
// from ProgramData and restrictDir; on Android it is the app's own.
func prepareDataDir(dir string) error { return os.MkdirAll(dir, 0o700) }
