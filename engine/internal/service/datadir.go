package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The service's data directory (C:\ProgramData\CoreShift) takes its
// permissions from ProgramData, where every user may create files and
// folders. Whoever creates one owns it and can change it, and the service,
// which runs as SYSTEM, reads its journal, rule sets and update files from
// there. SecureDataDir closes it: the directory belongs to SYSTEM and
// administrators, users may only read the directory itself, and what a user
// created in it is taken back or, where the service can make it anew,
// removed.

// regenerable are the subdirectories whose content the service can make
// again: one a user had a hand in is removed rather than trusted.
var regenerable = []string{"updates", "rules", "work", "tun"}

// dirSecurity is what SecureDataDir needs from the system; tests fake it.
type dirSecurity interface {
	// trusted reports whether path belongs to SYSTEM or administrators.
	trusted(path string) (bool, error)
	// lock makes dir SYSTEM's, open to administrators, readable (the
	// directory alone) by users, and not inheriting anything.
	lock(dir string) error
	// reclaim makes path SYSTEM's, with permissions inherited from its
	// directory only.
	reclaim(path string) error
	// isLink reports a symbolic link, junction or other reparse point.
	isLink(fi fs.FileInfo) bool
}

// secureDataDir is SecureDataDir with the system's operations in sec. It
// returns what it changed, for the log.
func secureDataDir(root string, sec dirSecurity) ([]string, error) {
	var notes []string
	fi, err := os.Lstat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	case sec.isLink(fi) || !fi.IsDir():
		// A link made before the first install would lead the service's
		// files anywhere.
		if err := os.Remove(root); err != nil {
			return nil, fmt.Errorf("remove %s, which is not a directory: %w", root, err)
		}
		notes = append(notes, "removed "+root+", a link or file in place of the data directory")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return notes, err
	}
	// From here on no user can add or rename anything in root.
	if err := sec.lock(root); err != nil {
		return notes, fmt.Errorf("secure %s: %w", root, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return notes, err
	}
	var errs []error
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		fi, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if isRegenerable(e.Name()) {
			if ok, err := trustedTree(sec, path, fi); err != nil || !ok {
				if err := os.RemoveAll(path); err != nil {
					errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
					continue
				}
				notes = append(notes, "removed "+path+": made or changed by another user")
			}
			continue
		}
		n, err := reclaimTree(path, fi, sec)
		notes = append(notes, n...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return notes, errors.Join(errs...)
}

// reclaimTree takes back path and what is in it from other owners and
// removes the links in it. A directory is taken back before its content is
// looked at, so its owner can no longer change what is in it meanwhile.
func reclaimTree(path string, fi fs.FileInfo, sec dirSecurity) ([]string, error) {
	if sec.isLink(fi) {
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove the link %s: %w", path, err)
		}
		return []string{"removed the link " + path}, nil
	}
	var notes []string
	ok, err := sec.trusted(path)
	if err != nil {
		return nil, err
	}
	if !ok {
		if err := sec.reclaim(path); err != nil {
			return nil, fmt.Errorf("take back %s: %w", path, err)
		}
		notes = append(notes, "took back "+path+" from another owner")
	}
	if !fi.IsDir() {
		return notes, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return notes, err
	}
	var errs []error
	for _, e := range entries {
		p := filepath.Join(path, e.Name())
		cfi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		n, err := reclaimTree(p, cfi, sec)
		notes = append(notes, n...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return notes, errors.Join(errs...)
}

func isRegenerable(name string) bool {
	for _, r := range regenerable {
		if strings.EqualFold(name, r) {
			return true
		}
	}
	return false
}

// trustedTree reports whether the tree at path is trusted: it holds no
// links, and everything in it belongs to SYSTEM or administrators.
func trustedTree(sec dirSecurity, path string, fi fs.FileInfo) (bool, error) {
	if sec.isLink(fi) {
		return false, nil
	}
	ok, err := sec.trusted(path)
	if err != nil || !ok || !fi.IsDir() {
		return ok, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		p := filepath.Join(path, e.Name())
		cfi, err := os.Lstat(p)
		if err != nil {
			return false, err
		}
		if ok, err := trustedTree(sec, p, cfi); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}
