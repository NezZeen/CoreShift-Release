package apps

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func running() ([]App, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []App
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		path, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			continue // kernel threads, or not ours to see
		}
		path = strings.TrimSuffix(path, " (deleted)")
		out = append(out, App{Name: filepath.Base(path), Path: path})
	}
	return out, nil
}

func isSystem(path string) bool {
	for _, dir := range []string{"/usr/lib/", "/usr/libexec/", "/lib/", "/usr/sbin/", "/sbin/"} {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}
	return false
}
