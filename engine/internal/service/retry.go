package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"coreshift/engine/internal/store"
	"coreshift/engine/internal/subscription"
)

// Downloads that failed for want of a route are tried again once the VPN
// is up. Without a connection a subscription refresh or the app's update
// check has only the direct way, which a mobile operator's white list or
// a network halfway through connecting cuts off; through the tunnel the
// same request usually passes. Each failure is retried once: a retry made
// through the tunnel is not untunneled, so its failure waits for the
// usual schedule (store.NextRefresh, appUpdateEvery).

// retryAfterConnectDelay is how long after Connected the retries run: the
// tunnel's DNS and routes settle, and a connection that drops at once is
// not worth it.
const retryAfterConnectDelay = 5 * time.Second

// subscriptionTimeout bounds one attempt at a subscription, as the
// store's own client did.
const subscriptionTimeout = 30 * time.Second

// untunneled is the error of a request that only had the direct way.
type untunneled struct{ err error }

func (e *untunneled) Error() string { return e.err.Error() }
func (e *untunneled) Unwrap() error { return e.err }

// retryable reports an error worth a retry through the tunnel: a request
// made without it that found no route to its server or no address for it.
func retryable(err error) bool {
	var u *untunneled
	if !errors.As(err, &u) {
		return false
	}
	var dnsErr *net.DNSError
	var op *net.OpError
	return errors.As(err, &dnsErr) || errors.As(err, &op) && op.Op == "dial"
}

// afterConnect holds what waits for the next Connected.
type afterConnect struct {
	mu     sync.Mutex
	subs   map[string]bool // subscription IDs
	update bool            // the app's update check
}

// noteSubscription records a refresh that failed for want of a route. One
// that worked since is told by the subscription's error (retryAfterConnect).
func (s *Service) noteSubscription(c store.Change) {
	r := &s.retry
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case c.What == "subscription-removed":
		delete(r.subs, c.ID)
	case c.What == "subscription-updated" && retryable(c.Err):
		if r.subs == nil {
			r.subs = map[string]bool{}
		}
		r.subs[c.ID] = true
	}
}

// noteUpdateCheck records how the app's update check ended.
func (s *Service) noteUpdateCheck(err error) {
	s.retry.mu.Lock()
	s.retry.update = retryable(err)
	s.retry.mu.Unlock()
}

// retryAfterConnect runs, a little after the connection came up, what
// failed before for want of a route. What finds the connection gone by
// then waits for the next one.
func (s *Service) retryAfterConnect() {
	r := &s.retry
	r.mu.Lock()
	subs, update := r.subs, r.update
	r.subs, r.update = nil, false
	r.mu.Unlock()
	if len(subs) == 0 && !update {
		return
	}
	go func() {
		time.Sleep(s.cfg.retryDelay)
		if s.Status().State != Connected {
			r.mu.Lock()
			for id := range subs {
				if r.subs == nil {
					r.subs = map[string]bool{}
				}
				r.subs[id] = true
			}
			r.update = r.update || update
			r.mu.Unlock()
			return
		}
		if update {
			s.CheckAppUpdate() // answers at once; RunAppUpdates checks
		}
		if st := s.cfg.Store; st != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			for id := range subs {
				if sub, ok := st.Subscription(id); ok && sub.LastError != "" {
					st.Refresh(ctx, id) // the outcome is stored and reported
				}
			}
		}
	}()
}

// fetchSubscription downloads a subscription the way the store refreshes
// them (viaDirectOrProxy).
func (s *Service) fetchSubscription(ctx context.Context, rawURL, userAgent string) (subscription.Fetched, error) {
	return store.FetchVia(ctx, s.subscriptionVia, rawURL, userAgent, false)
}

// subscriptionVia is the store's way to the panels.
func (s *Service) subscriptionVia(ctx context.Context, do func(*http.Client) error) error {
	return s.viaDirectOrProxy(ctx, subscriptionTimeout, do)
}
