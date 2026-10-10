package store

import (
	"context"
	"errors"
	"slices"
	"strings"

	"coreshift/engine/internal/subscription"
)

// Edit changes a subscription's label, URL or User-Agent; nil fields are
// left alone. A new URL or User-Agent is fetched before it is saved.
type Edit struct {
	Name      *string `json:"name"`
	URL       *string `json:"url"`
	UserAgent *string `json:"user_agent"`
	// Content replaces the nodes of a pasted list.
	Content *string `json:"content"`
}

func (s *Store) Edit(ctx context.Context, id string, e Edit) (Subscription, error) {
	sub, ok := s.Subscription(id)
	if !ok {
		return Subscription{}, ErrNotFound
	}
	refetch := false
	if e.Name != nil {
		sub.Name = strings.TrimSpace(*e.Name)
	}
	if e.UserAgent != nil {
		ua := strings.TrimSpace(*e.UserAgent)
		refetch = refetch || ua != sub.UserAgent
		sub.UserAgent = ua
	}
	if e.URL != nil {
		u := strings.TrimSpace(*e.URL)
		if sub.URL == "" {
			return Subscription{}, errors.New("a pasted list has no URL; add the subscription anew")
		}
		if err := checkURL(u); err != nil {
			return Subscription{}, err
		}
		refetch = refetch || u != sub.URL
		if subscription.PanelHost(u) != subscription.PanelHost(sub.URL) {
			// Another panel: it has never seen the machine-wide id.
			sub.HWIDScope = HWIDPanel
		}
		sub.URL = u
	}
	if e.Content != nil {
		if sub.URL != "" {
			return Subscription{}, errors.New("the nodes of a URL subscription come from its panel")
		}
		if err := s.load(ctx, &sub, *e.Content); err != nil {
			return Subscription{}, err
		}
	} else if refetch && sub.URL != "" {
		if err := s.load(ctx, &sub, ""); err != nil {
			return Subscription{}, err
		}
	}
	err := s.modify(Change{What: "subscription-updated", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		if sub.URL != d.Subscriptions[i].URL &&
			slices.ContainsFunc(d.Subscriptions, func(o Subscription) bool { return o.URL == sub.URL }) {
			return ErrExists
		}
		d.Subscriptions[i] = sub
		repointSelection(d, &sub)
		return nil
	})
	return sub, err
}

// Remove deletes a subscription, and the selection if it pointed there.
func (s *Store) Remove(id string) error {
	return s.modify(Change{What: "subscription-removed", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		d.Subscriptions = slices.Delete(d.Subscriptions, i, i+1)
		if d.Selection != nil && d.Selection.Subscription == id {
			d.Selection = nil
		}
		return nil
	})
}

// Move puts subscription id at position index in the list.
func (s *Store) Move(id string, index int) error {
	return s.modify(Change{What: "subscription-updated", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		sub := d.Subscriptions[i]
		d.Subscriptions = slices.Delete(d.Subscriptions, i, i+1)
		index = min(max(index, 0), len(d.Subscriptions))
		d.Subscriptions = slices.Insert(d.Subscriptions, index, sub)
		return nil
	})
}
