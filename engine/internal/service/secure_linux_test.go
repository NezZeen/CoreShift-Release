//go:build linux && !android

package service

import (
	"os"
	"path/filepath"
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
