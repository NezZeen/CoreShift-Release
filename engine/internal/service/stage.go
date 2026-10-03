package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The installer runs as SYSTEM. Checking its hash by path and then running
// it by path leaves a moment to swap the file, and running it from the
// folder it was downloaded to lets whoever can write there plant DLLs next
// to it. So the verified download is copied into a new folder with a
// random name that only SYSTEM and administrators can write to, the copy is
// opened so that nobody can write, rename or delete it while it is open,
// its hash is checked through that very handle, and it runs from there
// with that folder as its working directory, the handle still open.

// stagePrefix names the folders installers run from, in the updates folder.
const stagePrefix = "run-"

// stagedInstaller is a verified copy of an installer, locked while open.
type stagedInstaller struct {
	path string
	dir  string
	f    *os.File
}

// Close lets go of the copy. Its folder stays until the next start: the
// installer may still be running from it.
func (s *stagedInstaller) Close() {
	if s != nil && s.f != nil {
		s.f.Close()
	}
}

// stageInstaller copies src into a new folder in parent and returns the
// copy, open and checked against sha256 (hex).
func stageInstaller(src, sum, parent string) (*stagedInstaller, error) {
	dir, err := os.MkdirTemp(parent, stagePrefix)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*stagedInstaller, error) {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := restrictDir(dir); err != nil {
		return fail(fmt.Errorf("secure %s: %w", dir, err))
	}
	dst := filepath.Join(dir, filepath.Base(src))
	if err := copyNew(src, dst); err != nil {
		return fail(err)
	}
	f, err := openLocked(dst)
	if err != nil {
		return fail(err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		f.Close()
		return fail(err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, sum) {
		f.Close()
		return fail(errors.New("the downloaded update changed or is gone; it will be downloaded again"))
	}
	return &stagedInstaller{path: dst, dir: dir, f: f}, nil
}

// copyNew copies src to dst, which must not exist yet.
func copyNew(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if serr := out.Sync(); err == nil {
		err = serr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// cleanStaged removes the folders of earlier installers, which ran by now.
func cleanStaged(parent string) {
	dirs, _ := filepath.Glob(filepath.Join(parent, stagePrefix+"*"))
	for _, d := range dirs {
		os.RemoveAll(d)
	}
}
