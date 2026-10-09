package store

import (
	"os"
	"path/filepath"
	"testing"
)

// The verbose journal is on by default; a file of 0.9.2, which saved it off
// for everyone, is moved on once; a user's choice after that stays.
func TestVerboseJournalByDefault(t *testing.T) {
	if d := Defaults().Log; !d.Verbose || d.Version != logVersion {
		t.Fatalf("defaults = %+v", d)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	for _, c := range []struct {
		name, file string
		want       bool
	}{
		{"0.9.2, off", `{"version":1,"settings":{"tun":true,"log":{"verbose":false}}}`, true},
		{"before 0.9.2", `{"version":1,"settings":{"tun":true}}`, true},
		{"turned off by the user", `{"version":1,"settings":{"tun":true,"log":{"verbose":false,"version":1}}}`, false},
	} {
		if err := os.WriteFile(path, []byte(c.file), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Open(path, Options{})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := s.Settings().Log.Verbose; got != c.want {
			t.Errorf("%s: verbose = %v, want %v", c.name, got, c.want)
		}
	}
}
