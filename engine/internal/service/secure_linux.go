//go:build linux && !android

package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// File permissions on Linux.
//
// The daemon runs as root under systemd; the app runs as the signed-in
// user, who reaches the daemon with the token in api.json. As on Windows,
// where only local users may read that file, the token must not be for
// everyone: here only members of APIGroup may read it. The package adds
// the installing user to the group.
//
//	/var/lib/coreshift            root:coreshift 0710  members reach files by name, cannot list
//	/var/lib/coreshift/api.json   root:coreshift 0640  the address and token
//	everything else               root           0700 directories (state, work, tun, updates),
//	                                             0600 files (the daemon's umask is 077)
//
// Run by a user rather than root (development), the directory is that
// user's, and api.json is shared with the group only if the user may.
const APIGroup = "coreshift"

// prepareDataDir creates the data directory and, when running as root,
// gives it to root and APIGroup, so the group can open api.json in it. A
// symlink or a directory someone else owns is refused: root must not
// write its secrets through a path another user controls.
func prepareDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if os.Geteuid() != 0 {
		return nil
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid != 0 {
		return fmt.Errorf("%s belongs to uid %d, not root; remove it or fix its owner", dir, st.Uid)
	}
	gid, ok := apiGroupID()
	if !ok {
		// No group: only root can use the daemon. The package creates the
		// group; say so in the log rather than fail.
		fmt.Fprintf(os.Stderr, "warning: group %q does not exist; only root can read %s\n", APIGroup, filepath.Join(dir, "api.json"))
		return os.Chmod(dir, 0o700)
	}
	if err := os.Chown(dir, 0, gid); err != nil {
		return err
	}
	return os.Chmod(dir, dataDirMode)
}

// dataDirMode lets the group through the directory without listing it.
const dataDirMode fs.FileMode = 0o710

func apiGroupID() (int, bool) {
	g, err := user.LookupGroup(APIGroup)
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	return gid, err == nil
}

func restrictDir(dir string) error { return os.Chmod(dir, 0o700) }

// SecureDataDir makes root, the service's data directory, root's (with
// APIGroup let through to api.json) and takes back what others created in
// it, as on Windows (datadir.go): a link or file in its place is removed,
// a regenerable subdirectory someone else touched is removed, anything
// else is given back to root and closed to others. It returns what it
// changed, for the log. A run that is not root's (development) only
// creates the directory.
func SecureDataDir(root string) ([]string, error) {
	if os.Geteuid() != 0 {
		return nil, os.MkdirAll(root, 0o700)
	}
	return secureDataDir(root, linuxDirSecurity{})
}

// linuxDirSecurity is dirSecurity with owners and modes.
type linuxDirSecurity struct{}

func (linuxDirSecurity) trusted(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	// root's, and nobody else may write to it.
	return ok && st.Uid == 0 && fi.Mode().Perm()&0o022 == 0, nil
}

func (linuxDirSecurity) lock(dir string) error {
	if err := os.Chown(dir, 0, 0); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	// The group is let through last, once nobody else can change it.
	return prepareDataDir(dir)
}

func (linuxDirSecurity) reclaim(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := os.Lchown(path, 0, 0); err != nil {
		return err
	}
	return os.Chmod(path, fi.Mode().Perm()&0o700)
}

func (linuxDirSecurity) isLink(fi fs.FileInfo) bool { return fi.Mode()&fs.ModeSymlink != 0 }

// WriteShared writes a file the unprivileged UI must be able to read:
// owned by APIGroup and readable by it (0640), or the owner's alone (0600)
// where the group is missing or the process may not hand the file to it.
// It is written aside and renamed, so it is never readable before its
// permissions are set.
func WriteShared(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	mode := fs.FileMode(0o600)
	if gid, ok := apiGroupID(); ok && f.Chown(-1, gid) == nil {
		mode = 0o640
	}
	cerr := f.Chmod(mode)
	if err := errors.Join(werr, cerr, f.Close()); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
