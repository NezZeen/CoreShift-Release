package store

import (
	"context"
	"errors"
	"net/http"

	"coreshift/engine/internal/subscription"
)

// Refresh downloads subscription id again. On failure the old nodes are
// kept and the error is recorded.
func (s *Store) Refresh(ctx context.Context, id string) (Subscription, error) {
	sub, ok := s.Subscription(id)
	if !ok {
		return Subscription{}, ErrNotFound
	}
	if sub.URL == "" {
		return sub, errors.New("a pasted list has nothing to refresh")
	}
	fetchErr := s.load(ctx, &sub, "")
	sub.CheckedAt = s.opts.now()
	if fetchErr != nil {
		sub.LastError = fetchErr.Error()
	}
	err := s.modify(Change{What: "subscription-updated", ID: id, Err: fetchErr}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound // removed while fetching
		}
		cur := &d.Subscriptions[i]
		if fetchErr != nil {
			// The nodes stay; what the panel said about the subscription is
			// new, if it said anything.
			cur.Info, cur.CheckedAt, cur.LastError = sub.Info, sub.CheckedAt, sub.LastError
			return nil
		}
		// Keep edits made while fetching; take only what the fetch produced.
		cur.Info, cur.Format, cur.Nodes, cur.Skipped, cur.Auto = sub.Info, sub.Format, sub.Nodes, sub.Skipped, sub.Auto
		cur.UpdatedAt, cur.CheckedAt, cur.LastError = sub.UpdatedAt, sub.CheckedAt, ""
		repointSelection(d, cur)
		sub = *cur
		return nil
	})
	return sub, errors.Join(fetchErr, err)
}

// SetVia makes subscriptions reach their panels through via (the service
// adds the tunnel while it is up); nil goes back to Options.Client.
func (s *Store) SetVia(via Via) {
	if via == nil {
		s.via.Store(nil)
		return
	}
	s.via.Store(&via)
}

// FetchVia downloads a subscription as subscription.FetchAs does, trying
// the ways via offers until one gets servers. A panel that told about the
// subscription (its headers) answered, even with an error such as a
// message instead of servers: another way would get the same.
func FetchVia(ctx context.Context, via Via, rawURL, userAgent string, legacyHWID bool) (subscription.Fetched, error) {
	var f subscription.Fetched
	var answer error
	err := via(ctx, func(c *http.Client) error {
		var err error
		f, err = subscription.FetchAs(ctx, c, rawURL, userAgent, legacyHWID)
		if err != nil && len(f.Nodes) == 0 && f.Info == (subscription.Info{}) {
			return err
		}
		answer = err
		return nil
	})
	if err != nil {
		return subscription.Fetched{}, err
	}
	return f, answer
}

// load fills sub's nodes from its URL or, when content is set, from content.
func (s *Store) load(ctx context.Context, sub *Subscription, content string) error {
	var f subscription.Fetched
	var err error
	if content != "" {
		f.Result, err = subscription.Parse([]byte(content))
	} else {
		s.fetchMu.Lock()
		ua := sub.UserAgent
		if ua == "" {
			ua = s.Settings().Updates.UserAgent
		}
		f, err = s.opts.fetch(ctx, sub.URL, ua, sub.HWIDScope != HWIDPanel)
		s.fetchMu.Unlock()
		if err != nil && len(f.Nodes) == 0 {
			// A panel that sent a message instead of servers (subscription
			// expired, device limit) still says until when and where its
			// support is.
			if f.Info != (subscription.Info{}) {
				sub.Info = infoOf(f.Info)
			}
			return &FetchError{err}
		}
	}
	if err != nil && len(f.Nodes) == 0 {
		return err
	}
	sub.Info = infoOf(f.Info)
	sub.Format, sub.Nodes, sub.Skipped, sub.Auto = string(f.Format), f.Nodes, nil, f.Auto
	for _, sk := range f.Skipped {
		sub.Skipped = append(sub.Skipped, sk.String())
	}
	sub.UpdatedAt = s.opts.now()
	sub.CheckedAt, sub.LastError = sub.UpdatedAt, ""
	return nil
}

func infoOf(i subscription.Info) Info {
	return Info{
		Title: i.Title, Upload: i.Upload, Download: i.Download, Total: i.Total,
		Expire: i.Expire, UpdateIntervalHours: int(i.UpdateInterval.Hours()),
		SupportURL: i.SupportURL, WebPageURL: i.WebPageURL, Announce: i.Announce, AnnounceURL: i.AnnounceURL,
	}
}
