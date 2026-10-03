package apps

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// The Linux listing's logic, here so that it is tested on every OS.

// procRealUID parses the real uid from a /proc/<pid>/status.
func procRealUID(status []byte) (int, bool) {
	sc := bufio.NewScanner(bytes.NewReader(status))
	for sc.Scan() {
		v, ok := strings.CutPrefix(sc.Text(), "Uid:")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			return 0, false
		}
		uid, err := strconv.Atoi(f[0])
		return uid, err == nil
	}
	return 0, false
}

// nobody is the overflow uid, used by sandboxes rather than people.
const nobody = 65534

// personUID reports whether a uid belongs to a person rather than to the
// system: the distributions give people uids from 1000 up. self, the
// daemon's own uid, counts too unless it is root, for development runs.
func personUID(uid, self int) bool {
	return (uid >= 1000 && uid != nobody) || (uid == self && self != 0)
}

// linuxSystemDirs hold helpers and daemons a person would not route by
// hand. Applications under /usr/lib (firefox-esr) or /opt stay listed.
var linuxSystemDirs = []string{"/usr/libexec/", "/usr/lib/systemd/", "/lib/systemd/", "/usr/sbin/", "/sbin/", "/usr/lib/xorg/"}

func linuxSystemPath(path string) bool {
	for _, dir := range linuxSystemDirs {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}
	return false
}
