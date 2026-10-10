package store

import (
	"slices"
)

// Settings returns the current settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSettings(s.data.Settings)
}

// SetSettings validates, tidies and saves set, returning what was saved.
func (s *Store) SetSettings(set Settings) (Settings, error) {
	set, err := set.normalize()
	if err != nil {
		return Settings{}, err
	}
	err = s.modify(Change{What: "settings"}, func(d *fileData) error {
		d.Settings = set
		return nil
	})
	return cloneSettings(set), err
}

// Subscriptions returns every subscription in the user's order. Nodes are
// shared with the store and must not be modified.
func (s *Store) Subscriptions() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.Subscriptions)
}

func (s *Store) Subscription(id string) (Subscription, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Subscription{}, false
	}
	return s.data.Subscriptions[i], true
}

// Watch registers f to be called after every modification, outside the
// store's lock, on the goroutine that made it.
func (s *Store) Watch(f func(Change)) {
	s.mu.Lock()
	s.watchers = append(slices.Clip(s.watchers), f)
	s.mu.Unlock()
}
