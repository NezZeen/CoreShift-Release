package store

import (
	"context"
	"time"
)

// retryAfter is how soon a failed refresh is tried again.
const retryAfter = 15 * time.Minute

// RunUpdater refreshes due subscriptions until ctx ends: at start, then every
// tick. Failures are recorded on the subscription and reported to watchers.
func (s *Store) RunUpdater(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		s.RefreshDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RefreshDue refreshes every subscription whose update is due and returns
// how many it tried.
func (s *Store) RefreshDue(ctx context.Context) int {
	set := s.Settings()
	if !set.Updates.Auto {
		return 0
	}
	tried := 0
	for _, sub := range s.Subscriptions() {
		if ctx.Err() != nil {
			break
		}
		if s.due(&sub, set, s.opts.now()) {
			tried++
			s.Refresh(ctx, sub.ID) // the outcome is stored and reported
		}
	}
	return tried
}

// Interval is how often sub is refreshed: what the panel asks for, else the
// user's setting.
func (sub *Subscription) Interval(set Settings) time.Duration {
	if h := sub.Info.UpdateIntervalHours; h > 0 {
		return time.Duration(h) * time.Hour
	}
	return time.Duration(set.Updates.IntervalHours) * time.Hour
}

func (s *Store) due(sub *Subscription, set Settings, now time.Time) bool {
	next := NextRefresh(sub, set)
	return !next.IsZero() && !now.Before(next)
}

// NextRefresh is when sub is due for a background refresh; zero when it
// never is (a pasted list, or auto-update off).
func NextRefresh(sub *Subscription, set Settings) time.Time {
	if sub.URL == "" || !set.Updates.Auto {
		return time.Time{}
	}
	interval := sub.Interval(set)
	if sub.LastError != "" {
		return sub.CheckedAt.Add(min(retryAfter, interval))
	}
	return sub.UpdatedAt.Add(interval)
}
