//go:build linux && !android

package apps

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// running lists the programs of the people signed in: the daemon runs as
// root and sees every process, but routing root's and services' own
// processes is not what the list is for.
func running() ([]App, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	self := os.Getuid()
	var out []App
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", e.Name())
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil {
			continue // gone meanwhile
		}
		if uid, ok := procRealUID(status); !ok || !personUID(uid, self) {
			continue
		}
		path, err := os.Readlink(filepath.Join(dir, "exe"))
		if err != nil {
			continue // kernel threads, or not ours to see
		}
		path = strings.TrimSuffix(path, " (deleted)")
		out = append(out, App{Name: filepath.Base(path), Path: path})
	}
	return out, nil
}

func isSystem(path string) bool { return linuxSystemPath(path) }
