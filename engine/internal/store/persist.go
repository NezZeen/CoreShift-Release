package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"coreshift/engine/internal/fsutil"
)

// modify applies f and saves. When f fails nothing changes; when saving
// fails the change is undone too, unless a later one was made meanwhile,
// which then carries it to the file.
//
// The new state is encoded under mu, in the order of the changes, and
// written outside it, so that readers never wait for the disk; a write
// never replaces a later state on disk with an earlier one.
func (s *Store) modify(c Change, f func(*fileData) error) error {
	s.mu.Lock()
	orig, origRaw := s.data, s.raw
	s.data = cloneData(s.data)
	err := f(&s.data)
	var b []byte
	if err == nil {
		for i := range s.data.Subscriptions {
			s.data.Subscriptions[i].Fingerprints()
		}
		b, err = s.encode()
	}
	if err != nil {
		s.data = orig
		s.mu.Unlock()
		return err
	}
	s.raw = b
	s.seq++
	seq := s.seq
	watchers := s.watchers
	s.mu.Unlock()

	if err := s.write(seq, b); err != nil {
		s.mu.Lock()
		if s.seq == seq {
			s.data, s.raw = orig, origRaw
		}
		s.mu.Unlock()
		return err
	}
	for _, w := range watchers {
		w(c)
	}
	return nil
}

func (s *Store) index(id string) int {
	return slices.IndexFunc(s.data.Subscriptions, func(sub Subscription) bool { return sub.ID == id })
}

// encode returns the file for the current state, with the fields of later
// versions carried over. Call it with mu held.
func (s *Store) encode() ([]byte, error) {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return nil, err
	}
	return keepUnknown(b, s.raw, reflect.TypeOf(s.data))
}

// write saves state seq atomically: a crash leaves the old or the new
// file. A state older than the one on disk is not written.
func (s *Store) write(seq uint64, b []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if seq <= s.written {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := fsutil.WriteAtomic(s.path, b, 0o600); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	s.written = seq
	return nil
}
