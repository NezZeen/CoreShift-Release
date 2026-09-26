package service

import (
	"os"

	"golang.org/x/sys/windows"
)

const (
	// Only SYSTEM and Administrators; not inherited from ProgramData, where
	// every user may read.
	sddlPrivate = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	// Readable by local users, so the unprivileged UI can read the API token.
	sddlShared = "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)"
)

// restrictDir limits dir to SYSTEM and Administrators. It is skipped when not
// elevated (development runs), where it would lock the current user out.
func restrictDir(dir string) error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return nil
	}
	return setDACL(dir, sddlPrivate)
}

// WriteShared writes a file the unprivileged UI must be able to read.
func WriteShared(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return nil
	}
	return setDACL(path, sddlShared)
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
