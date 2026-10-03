//go:build linux && !android

package service

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// These run on Linux only, without root: a development run's directory
// stays the user's, and api.json is never readable by others.

func TestWriteSharedLinux(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.json")
	if err := WriteShared(path, []byte(`{"token":"x"}`)); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 && perm != 0o640 {
		t.Fatalf("api.json mode %o, want 0600 or 0640", perm)
	}
	if fi.Mode().Perm()&0o007 != 0 {
		t.Fatal("api.json readable by others")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
	// Rewritten on every start.
	if err := WriteShared(path, []byte(`{"token":"y"}`)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != `{"token":"y"}` {
		t.Fatalf("content %q", b)
	}
}

func TestPrepareDataDirLinux(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("as root the directory goes to root and the coreshift group")
	}
	dir := filepath.Join(t.TempDir(), "coreshift")
	if err := prepareDataDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("data dir %v, %v", fi.Mode(), err)
	}
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o600)
	if prepareDataDir(file) == nil {
		t.Fatal("a file accepted as the data directory")
	}
}

// As root (the WSL and CI runs): what another user planted in the data
// directory is taken back or removed.
func TestSecureDataDirLinux(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	root := filepath.Join(t.TempDir(), "coreshift")
	os.MkdirAll(filepath.Join(root, "work"), 0o777)
	os.WriteFile(filepath.Join(root, "work", "planted.json"), nil, 0o666)
	os.WriteFile(filepath.Join(root, "traffic.json"), []byte("{}"), 0o666)
	os.Symlink("/etc/shadow", filepath.Join(root, "dnsguard.json"))
	for _, p := range []string{"work", "work/planted.json", "traffic.json"} {
		os.Lchown(filepath.Join(root, p), 65534, 65534)
	}
	notes, err := SecureDataDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "work")); !os.IsNotExist(err) {
		t.Error("a work directory another user made was kept")
	}
	if _, err := os.Lstat(filepath.Join(root, "dnsguard.json")); !os.IsNotExist(err) {
		t.Error("a planted link was kept")
	}
	fi, err := os.Stat(filepath.Join(root, "traffic.json"))
	if err != nil || fi.Sys().(*syscall.Stat_t).Uid != 0 || fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("traffic.json not taken back: %v %v", fi.Mode(), err)
	}
	if fi, _ := os.Stat(root); fi.Mode().Perm()&0o007 != 0 {
		t.Errorf("data directory open to others: %v", fi.Mode())
	}
	if len(notes) == 0 {
		t.Error("no notes for the log")
	}
}
