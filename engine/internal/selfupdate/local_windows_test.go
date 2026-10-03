package selfupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckLocal(t *testing.T) {
	dir := t.TempDir()
	if err := checkLocal(dir); err != nil {
		t.Errorf("a local folder: %v", err)
	}
	for _, p := range []string{`\\host\share\releases`, `\\?\C:\releases`, `\\.\C:\releases`} {
		if err := checkLocal(p); err == nil {
			t.Errorf("%s accepted", p)
		}
	}
	// A junction anywhere on the way may lead to a share.
	target := t.TempDir()
	link := filepath.Join(dir, "releases")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(target, ManifestName), []byte("{}"), 0o600)
	if err := checkLocal(filepath.Join(link, ManifestName)); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("through a junction: %v", err)
	}
	src, err := ParseSource(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(context.Background(), nil, src, ManifestName, nil); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("Check through a junction: %v", err)
	}
}
