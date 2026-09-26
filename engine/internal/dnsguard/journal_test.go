package dnsguard

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestJournalUndoNewestFirstAndKeepsFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.json")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"a", "b", "c"} {
		if err := j.Record(k, map[string]string{"k": k}); err != nil {
			t.Fatal(err)
		}
	}

	reopened, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Len() != 3 {
		t.Fatalf("reopened journal has %d changes, want 3", reopened.Len())
	}

	var order []string
	err = reopened.Undo(func(c Change) error {
		order = append(order, c.Kind)
		if c.Kind == "b" {
			return errors.New("boom")
		}
		return nil
	})
	if err == nil {
		t.Fatal("Undo should report the failed change")
	}
	if want := []string{"c", "b", "a"}; !slices.Equal(order, want) {
		t.Fatalf("undo order %v, want %v", order, want)
	}
	if kinds := kindsOf(reopened); !slices.Equal(kinds, []string{"b"}) {
		t.Fatalf("remaining %v, want [b]", kinds)
	}

	if err := reopened.Undo(func(Change) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty journal should be removed from disk, stat err = %v", err)
	}
}

func TestJournalCorruptIsMovedAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if j.Len() != 0 {
		t.Fatalf("corrupt journal loaded %d changes", j.Len())
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("corrupt journal not moved aside: %v", err)
	}
}

func kindsOf(j *Journal) []string {
	var out []string
	for _, c := range j.Changes() {
		out = append(out, c.Kind)
	}
	return out
}
