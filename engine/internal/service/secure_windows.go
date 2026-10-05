package service

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// Only SYSTEM and Administrators; not inherited from ProgramData, where
	// every user may read.
	sddlPrivate = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	// The data directory: SYSTEM and Administrators, and everything in it
	// after them; users may list the directory itself (to find api.json,
	// so that the app tells a stopped service from a refused file),
	// nothing more, and nothing in it inherits that. api.json itself is
	// for the API group only (sharedSDDL, apigroup_windows.go).
	sddlRoot = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;;0x1200a9;;;BU)"
)

func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// restrictDir limits dir to SYSTEM and Administrators. It is skipped when not
// elevated (development runs), where it would lock the current user out.
func restrictDir(dir string) error {
	if !elevated() {
		return nil
	}
	return setDACL(dir, sddlPrivate)
}

// WriteShared writes a file the unprivileged UI must be able to read: the
// members of APIGroupName may, besides SYSTEM and Administrators. The
// file is always made anew, by the service, and replaces the old one: a
// file someone else created would stay theirs, to rewrite at will.
func WriteShared(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil && elevated() {
		werr = setDACL(name, currentSharedSDDL())
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		os.Remove(name)
	}
	return werr
}

// keepSharedEvery is how often KeepShared looks at the API group.
const keepSharedEvery = 15 * time.Second

// KeepShared keeps the permissions of a file WriteShared wrote up to date
// while ctx lasts: a user added to the API group may read it within
// keepSharedEvery, one removed no longer, without signing out and in and
// without a restart of the service.
func KeepShared(ctx context.Context, path string) {
	if !elevated() {
		return
	}
	last := "" // applied again at the first tick: the group may have changed since WriteShared
	t := time.NewTicker(keepSharedEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if sddl := currentSharedSDDL(); sddl != last && setDACL(path, sddl) == nil {
			last = sddl
		}
	}
}

func setDACL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// inheritDACL drops path's own permissions: it gets those of its directory.
func inheritDACL(path string) error {
	sd, err := windows.SecurityDescriptorFromString("D:")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// setOwnerSystem makes SYSTEM the owner of path. The owner may always
// change an object's permissions, whatever they say.
func setOwnerSystem(path string) error {
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, system, nil, nil, nil)
}

func ownerOf(path string) (*windows.SID, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return nil, err
	}
	owner, _, err := sd.Owner()
	return owner, err
}

func trustedOwner(sid *windows.SID) bool {
	return sid != nil && (sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}

type winDirSecurity struct{}

func (winDirSecurity) trusted(path string) (bool, error) {
	owner, err := ownerOf(path)
	if err != nil {
		return false, err
	}
	return trustedOwner(owner), nil
}

func (winDirSecurity) lock(dir string) error {
	if err := setOwnerSystem(dir); err != nil {
		return err
	}
	return setDACL(dir, sddlRoot)
}

func (winDirSecurity) reclaim(path string) error {
	if err := setOwnerSystem(path); err != nil {
		return err
	}
	return inheritDACL(path)
}

func (winDirSecurity) isLink(fi fs.FileInfo) bool {
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return d.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0
}

var privilegesOnce sync.Once

// enablePrivileges lets the service take ownership of what another user
// created and denied it access to. SYSTEM holds these privileges, off.
func enablePrivileges() {
	privilegesOnce.Do(func() {
		var tok windows.Token
		if windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok) != nil {
			return
		}
		defer tok.Close()
		for _, name := range []string{"SeTakeOwnershipPrivilege", "SeRestorePrivilege"} {
			var luid windows.LUID
			if windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(name), &luid) != nil {
				continue
			}
			tp := windows.Tokenprivileges{PrivilegeCount: 1}
			tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
			_ = windows.AdjustTokenPrivileges(tok, false, &tp, uint32(unsafe.Sizeof(tp)), nil, nil)
		}
	})
}

// SecureDataDir makes root, the service's data directory, SYSTEM's and
// closed to users, and takes back what other users created in it (see
// datadir.go). It returns what it changed, for the log. Development runs,
// not elevated, are left alone.
func SecureDataDir(root string) ([]string, error) {
	if !elevated() {
		return nil, os.MkdirAll(root, 0o700)
	}
	enablePrivileges()
	return secureDataDir(root, winDirSecurity{})
}
