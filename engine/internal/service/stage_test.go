package service

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStageInstaller(t *testing.T) {
	parent := t.TempDir()
	src := filepath.Join(t.TempDir(), "coreshift-setup.exe")
	if err := os.WriteFile(src, fakeInstaller, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(fakeInstaller)
	st, err := stageInstaller(src, hex.EncodeToString(sum[:]), parent)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(st.dir) != parent || !strings.HasPrefix(filepath.Base(st.dir), stagePrefix) ||
		filepath.Base(st.path) != "coreshift-setup.exe" || filepath.Dir(st.path) != st.dir {
		t.Errorf("staged at %s in %s", st.path, st.dir)
	}
	if b, _ := os.ReadFile(st.path); string(b) != string(fakeInstaller) {
		t.Errorf("copy = %q", b)
	}
	// Changing the download now changes nothing that runs.
	os.WriteFile(src, []byte("MZ evil"), 0o600)
	if runtime.GOOS == "windows" {
		// While open, the copy can be read (to start it) but not written,
		// renamed or deleted.
		if f, err := os.OpenFile(st.path, os.O_WRONLY, 0); err == nil {
			f.Close()
			t.Error("the staged installer could be opened for writing")
		}
		if err := os.Rename(st.path, st.path+".x"); err == nil {
			t.Error("the staged installer could be renamed")
		}
		if err := os.Remove(st.path); err == nil {
			t.Error("the staged installer could be deleted")
		}
	}
	st.Close()
	cleanStaged(parent)
	if left, _ := filepath.Glob(filepath.Join(parent, stagePrefix+"*")); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestStageInstallerRefusesAChangedFile(t *testing.T) {
	parent := t.TempDir()
	src := filepath.Join(t.TempDir(), "coreshift-setup.exe")
	os.WriteFile(src, []byte("MZ evil"), 0o600)
	sum := sha256.Sum256(fakeInstaller)
	if _, err := stageInstaller(src, hex.EncodeToString(sum[:]), parent); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("err = %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(parent, "*")); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}
