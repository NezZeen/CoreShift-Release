package store

import (
	"errors"
	"fmt"
	"slices"
)

// maxHidden bounds the servers hidden in one subscription, so that a
// request cannot grow the file without end.
const maxHidden = 10000

// SetHidden removes the servers with fingerprints fps from subscription
// id's list (hidden) or brings them back. A server to hide must be in the
// list; one to bring back need not be, so that what a panel dropped can be
// cleared too. The fingerprints stay when a refresh replaces the nodes.
func (s *Store) SetHidden(id string, fps []string, hidden bool) (Subscription, error) {
	var sub Subscription
	err := s.modify(Change{What: "subscription-updated", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		cur := &d.Subscriptions[i]
		// A new slice: the old one is shared with readers.
		out := slices.Clone(cur.Hidden)
		for _, fp := range fps {
			has := slices.Contains(out, fp)
			switch {
			case hidden && !has:
				if !slices.Contains(cur.Fingerprints(), fp) {
					return fmt.Errorf("subscription has no node %s", fp)
				}
				out = append(out, fp)
			case !hidden && has:
				out = slices.DeleteFunc(out, func(h string) bool { return h == fp })
			}
		}
		if len(out) > maxHidden {
			return errors.New("too many hidden servers")
		}
		if len(out) == 0 {
			out = nil
		}
		cur.Hidden = out
		sub = *cur
		return nil
	})
	return sub, err
}
