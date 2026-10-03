package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// checkLocal makes sure that reading path does not leave this computer.
// The service reads update folders as SYSTEM, and a folder on a network
// share, or a link or junction leading to one, would make it sign in to
// that host with the computer's account. path must be on a local drive
// letter that is a disk volume (not a network drive, nor a subst of one),
// and no part of it may be a link; each part is looked at without
// following it, so nothing is opened over the network to find out.
func checkLocal(path string) error {
	path = filepath.Clean(path)
	vol := filepath.VolumeName(path)
	if len(vol) != 2 || vol[1] != ':' {
		return fmt.Errorf("%s is not on a drive of this computer", path)
	}
	target := make([]uint16, 1024)
	n, err := windows.QueryDosDevice(windows.StringToUTF16Ptr(vol), &target[0], uint32(len(target)))
	if err != nil {
		return fmt.Errorf("%s: %w", vol, err)
	}
	dev := windows.UTF16ToString(target[:n])
	if !strings.HasPrefix(dev, `\Device\HarddiskVolume`) {
		return fmt.Errorf("%s is not a local disk (%s)", vol, dev)
	}
	switch windows.GetDriveType(windows.StringToUTF16Ptr(vol + `\`)) {
	case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE:
	default:
		return fmt.Errorf("%s is not a local disk", vol)
	}
	rest := strings.TrimPrefix(path, vol)
	cur := vol + `\`
	for _, part := range strings.Split(rest, `\`) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok && d.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("%s is a link; use the folder it leads to", cur)
		}
	}
	return nil
}
