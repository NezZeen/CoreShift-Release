//go:build !windows

package subscription

import (
	"os"
	"strings"
)

// platformDevice reads systemd's machine ID and the DMI product name. Android
// has neither; its app passes an ID through SetDeviceID.
func platformDevice() (id, version, model string) {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if id = readTrimmed(p); id != "" {
			break
		}
	}
	for _, line := range strings.Split(readTrimmed("/etc/os-release"), "\n") {
		if v, ok := strings.CutPrefix(line, "VERSION_ID="); ok {
			version = strings.Trim(v, `"`)
		}
	}
	model = strings.TrimSpace(readTrimmed("/sys/class/dmi/id/sys_vendor") + " " + readTrimmed("/sys/class/dmi/id/product_name"))
	return id, version, model
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
