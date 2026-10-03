package apps

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunningIncludesThisTest(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("not implemented here")
	}
	if runtime.GOOS == "linux" && os.Getuid() == 0 {
		t.Skip("root's own processes are left out of the list")
	}
	list, err := Running()
	if err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	found := false
	for i, a := range list {
		if strings.EqualFold(a.Name, filepath.Base(self)) {
			found = true
		}
		if i > 0 && strings.ToLower(list[i-1].Name) >= strings.ToLower(a.Name) {
			t.Errorf("not sorted or duplicated: %s, %s", list[i-1].Name, a.Name)
		}
		if isSystem(a.Path) {
			t.Errorf("system program listed: %s", a.Path)
		}
	}
	if !found {
		t.Errorf("%s not among %d programs", filepath.Base(self), len(list))
	}
}
