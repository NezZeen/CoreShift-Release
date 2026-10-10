package sysproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"coreshift/engine/internal/fsutil"
)

// Journal is what a proxy set by CoreShift replaced: written before
// anything changes, removed once all of it is back.
type Journal struct {
	// Addr is the port the proxy points at, for Logon and Guard.
	Addr    string    `json:"addr"`
	At      time.Time `json:"at"`
	Entries []Entry   `json:"entries"`
}

// Entry is one backend's settings before CoreShift's, and CoreShift's.
type Entry struct {
	Backend  string   `json:"backend"`
	Previous Settings `json:"previous"`
	Ours     Settings `json:"ours"`
	// Restoring is set before Previous is written back: a write that
	// failed part way leaves settings that are neither, which the next
	// Restore still puts back.
	Restoring bool `json:"restoring,omitempty"`
}

func (j *Journal) entry(backend string) *Entry {
	for i := range j.Entries {
		if j.Entries[i].Backend == backend {
			return &j.Entries[i]
		}
	}
	return nil
}

func (j *Journal) drop(backend string) {
	j.Entries = slices.DeleteFunc(j.Entries, func(e Entry) bool { return e.Backend == backend })
}

// loadJournal reads the journal at path; a missing one is empty. One that
// cannot be read as a journal is moved aside and reported: what it held
// is lost, and Apply then takes CoreShift's own settings for no proxy.
func loadJournal(path string) (Journal, error) {
	var j Journal
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return j, fmt.Errorf("read journal: %w", err)
	}
	if err := json.Unmarshal(b, &j); err != nil {
		os.Rename(path, path+".corrupt")
		return Journal{}, fmt.Errorf("journal %s is damaged: %w", path, err)
	}
	return j, nil
}

func saveJournal(path string, j Journal) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, b, 0o600)
}

func removeJournal(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
