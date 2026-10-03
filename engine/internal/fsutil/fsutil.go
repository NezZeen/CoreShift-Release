// Package fsutil holds the file helpers the engine's state files share.
package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteAtomic replaces path with data so that readers, and a crash at any
// moment, see either the old file or the new one, never a part of it.
//
// The data goes to a temporary file with a random name in path's directory
// (a fixed name could be created beforehand by someone else, as a link to
// elsewhere), which is synced and then renamed over path; where the system
// supports it, the directory is synced too, so the rename survives a crash.
// The directory must exist. perm is the new file's mode where the system
// has modes; on Windows the file takes the directory's permissions.
func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(perm); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	syncDir(dir)
	return nil
}
