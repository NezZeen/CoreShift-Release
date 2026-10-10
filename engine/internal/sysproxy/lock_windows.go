package sysproxy

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// lockFile holds path, opened without sharing, until release: a second
// guard cannot open it and leaves. ok is false when another holds it.
func lockFile(path string) (release func(), ok bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() { windows.CloseHandle(h) }, true, nil
}
