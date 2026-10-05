// Package apps lists the programs running on the computer, for choosing
// which of them bypass the tunnel.
package apps

import (
	"slices"
	"strings"
)

// App is a running program: its executable name and full path.
type App struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Running returns the programs running now, one entry per executable name,
// sorted by name. System programs are left out: routing them around the
// tunnel makes no sense, and the user would not recognise them.
func Running() ([]App, error) {
	list, err := running()
	if err != nil {
		return nil, err
	}
	return tidy(list), nil
}

// own are CoreShift's programs: the window talks only to the service, and
// the service's own traffic never goes through the tunnel.
var own = map[string]bool{"coreshift": true, "coreshift.exe": true, "coreshiftd": true, "coreshiftd.exe": true}

func tidy(list []App) []App {
	seen := map[string]bool{}
	out := []App{}
	for _, a := range list {
		key := strings.ToLower(a.Name)
		if a.Name == "" || a.Path == "" || seen[key] || own[key] || isSystem(a.Path) {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b App) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}
