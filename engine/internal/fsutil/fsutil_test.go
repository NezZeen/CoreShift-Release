package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := WriteAtomic(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "two" {
		t.Fatalf("content %q", b)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", fi.Mode().Perm())
		}
	}
	// No temporary file is left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("left %d entries in the directory", len(entries))
	}
}

// A file planted under the old fixed temporary name is neither written
// through nor in the way.
func TestWriteAtomicIgnoresAPlantedTempName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dnsguard.json")
	planted := path + ".tmp"
	if err := os.WriteFile(planted, []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(planted); string(b) != "planted" {
		t.Errorf("the planted file was changed: %q", b)
	}
	if b, _ := os.ReadFile(path); string(b) != "journal" {
		t.Errorf("content %q", b)
	}
}

func TestWriteAtomicFailureLeavesOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := WriteAtomic(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(dir, "missing", "x.json"), []byte("x"), 0o600); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Errorf("content %q", b)
	}
}
