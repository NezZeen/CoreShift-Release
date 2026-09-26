package dnsguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Change is one reversible system modification. Data is kind-specific and
// holds whatever is needed to undo it, e.g. the previous registry value.
type Change struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// Journal is an on-disk log of changes that have not been undone yet.
//
// A change is recorded before it is made, so undo functions must tolerate
// changes that never actually happened.
type Journal struct {
	path    string
	mu      sync.Mutex
	changes []Change
}

// OpenJournal loads the journal at path. A missing file is an empty journal;
// an unparsable one is moved aside so the guard can still start.
func OpenJournal(path string) (*Journal, error) {
	j := &Journal{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dnsguard: read journal: %w", err)
	}
	if err := json.Unmarshal(b, &j.changes); err != nil {
		j.changes = nil
		if rerr := os.Rename(path, path+".corrupt"); rerr != nil {
			return nil, fmt.Errorf("dnsguard: journal %s is corrupt (%v) and cannot be moved aside: %w", path, err, rerr)
		}
	}
	return j, nil
}

// Len returns the number of changes not undone yet.
func (j *Journal) Len() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.changes)
}

// Changes returns a copy of the pending changes, oldest first.
func (j *Journal) Changes() []Change {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.changes)
}

// Record appends a change and persists the journal.
func (j *Journal) Record(kind string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("dnsguard: encode %s change: %w", kind, err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.changes = append(j.changes, Change{Kind: kind, Data: raw})
	if err := j.save(); err != nil {
		j.changes = j.changes[:len(j.changes)-1]
		return err
	}
	return nil
}

// Undo calls undo for every change, newest first. Changes whose undo fails
// stay in the journal so a later Recover can retry them.
func (j *Journal) Undo(undo func(Change) error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	var failed []Change
	var errs []error
	for i := len(j.changes) - 1; i >= 0; i-- {
		c := j.changes[i]
		if err := undo(c); err != nil {
			errs = append(errs, fmt.Errorf("undo %s: %w", c.Kind, err))
			failed = append(failed, c)
		}
	}
	slices.Reverse(failed)
	j.changes = failed
	return errors.Join(append(errs, j.save())...)
}

func (j *Journal) save() error {
	if len(j.changes) == 0 {
		if err := os.Remove(j.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("dnsguard: remove journal: %w", err)
		}
		return nil
	}
	b, err := json.MarshalIndent(j.changes, "", "  ")
	if err != nil {
		return fmt.Errorf("dnsguard: encode journal: %w", err)
	}
	if err := writeFileAtomic(j.path, b, 0o600); err != nil {
		return fmt.Errorf("dnsguard: write journal: %w", err)
	}
	return nil
}

// writeFileAtomic replaces path with data via a synced temp file and rename,
// so readers never observe a partially written file.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
