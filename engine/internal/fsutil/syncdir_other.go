//go:build !windows

package fsutil

import "os"

// syncDir makes a rename in dir durable. Failing to is not an error: the
// file itself is complete either way.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	d.Close()
}
