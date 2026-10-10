package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	"coreshift/engine/internal/node"
)

// AddRequest is a subscription URL or, for a pasted list, its content.
type AddRequest struct {
	Name      string
	URL       string
	Content   string
	UserAgent string
}

// Add fetches or parses the subscription and saves it only if it has nodes,
// so a mistyped URL never ends up in the list.
func (s *Store) Add(ctx context.Context, req AddRequest) (Subscription, error) {
	req.URL, req.Name, req.UserAgent = strings.TrimSpace(req.URL), strings.TrimSpace(req.Name), strings.TrimSpace(req.UserAgent)
	if (req.URL == "") == (strings.TrimSpace(req.Content) == "") {
		return Subscription{}, errors.New(`need either "url" or "content"`)
	}
	if req.URL != "" {
		if err := checkURL(req.URL); err != nil {
			return Subscription{}, err
		}
		for _, sub := range s.Subscriptions() {
			if sub.URL == req.URL {
				return Subscription{}, ErrExists
			}
		}
	}
	sub := Subscription{ID: newID(), Name: req.Name, URL: req.URL, UserAgent: req.UserAgent, AddedAt: s.opts.now()}
	if sub.URL != "" {
		sub.HWIDScope = HWIDPanel
	}
	if err := s.load(ctx, &sub, req.Content); err != nil {
		return Subscription{}, err
	}
	if unnamedPaste(sub) {
		for _, o := range s.Subscriptions() {
			if unnamedPaste(o) {
				return s.addToPaste(o.ID, sub)
			}
		}
	}
	err := s.modify(Change{What: "subscription-added", ID: sub.ID}, func(d *fileData) error {
		if sub.URL != "" && slices.ContainsFunc(d.Subscriptions, func(o Subscription) bool { return o.URL == sub.URL }) {
			return ErrExists // added by a concurrent request
		}
		d.Subscriptions = append(d.Subscriptions, sub)
		return nil
	})
	return sub, err
}

// unnamedPaste reports a pasted list without a name, shown as "Local nodes".
// Servers pasted one by one go into the first such list rather than making
// a list, all with the same name, per paste.
func unnamedPaste(sub Subscription) bool {
	return sub.URL == "" && sub.Name == "" && sub.Info.Title == ""
}

// addToPaste appends to the list id the servers of add it does not have.
func (s *Store) addToPaste(id string, add Subscription) (Subscription, error) {
	var sub Subscription
	err := s.modify(Change{What: "subscription-updated", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		sub = d.Subscriptions[i]
		var fresh []node.Node
		for _, n := range add.Nodes {
			if !slices.ContainsFunc(sub.Nodes, func(o node.Node) bool { return o.Fingerprint() == n.Fingerprint() && o.Name == n.Name }) {
				fresh = append(fresh, n)
			}
		}
		if len(fresh) == 0 {
			return ErrServersExist
		}
		// New slices: the old ones are shared with readers.
		sub.Nodes = slices.Concat(sub.Nodes, fresh)
		sub.Skipped = slices.Concat(sub.Skipped, add.Skipped)
		sub.UpdatedAt, sub.CheckedAt, sub.LastError = add.UpdatedAt, add.CheckedAt, ""
		d.Subscriptions[i] = sub
		return nil
	})
	return sub, err
}

func checkURL(u string) error {
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return errors.New("a subscription URL starts with https:// or http://")
	}
	if strings.ContainsAny(u, " \r\n\t") {
		return errors.New("a subscription URL cannot contain spaces")
	}
	return nil
}

func newID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
