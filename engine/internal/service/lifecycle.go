package service

import (
	"context"
	"errors"
	"sync"
	"time"
)

// AutoConnectEnabled reports whether the settings ask to connect the
// selected node on start.
func (s *Service) AutoConnectEnabled() bool {
	return s.cfg.Store != nil && s.cfg.Store.Settings().AutoConnect
}

// AutoConnect connects the selected node when the settings ask for it and
// nothing is connected yet. The network may not be up yet when CoreShift
// starts with the system, so failures are retried for a while unless the
// user connects or disconnects in the meantime. It returns once connected,
// or with the reason it gave up; nil too when there was nothing to do.
func (s *Service) AutoConnect(ctx context.Context) error {
	if !s.AutoConnectEnabled() {
		return nil
	}
	var err error
	for i, delay := range []time.Duration{0, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute} {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		// After our own failed attempt the state is Failed; anything else
		// means the user took over.
		if st := s.Status().State; (i == 0 && st != Idle) || (i > 0 && st != Failed) {
			return nil
		}
		err = s.ConnectSelected(ctx)
		if err == nil || errors.Is(err, ErrNoSelection) || errors.Is(err, ErrDisconnected) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

// appWatch counts the app's open event streams. The desktop daemon runs
// only while the app does (WaitAppGone), so that closing the app, however
// it ends, takes the VPN down with it rather than leaving it running
// unseen.
type appWatch struct {
	mu      sync.Mutex
	n       int
	ever    bool          // an app has attached since the start
	changed chan struct{} // closed and replaced on every attach and detach
}

// state returns the count, whether any app attached yet, and a channel
// closed on the next change.
func (w *appWatch) state() (int, bool, <-chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.changed == nil {
		w.changed = make(chan struct{})
	}
	return w.n, w.ever, w.changed
}

func (w *appWatch) add(d int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n += d
	w.ever = w.ever || d > 0
	if w.changed != nil {
		close(w.changed)
	}
	w.changed = make(chan struct{})
}

// AttachApp records an app connected until the returned func is called.
func (s *Service) AttachApp() (detach func()) {
	s.apps.add(1)
	var once sync.Once
	return func() { once.Do(func() { s.apps.add(-1) }) }
}

// WaitAppGone returns true once no app has been attached for grace, or,
// before any was, for first; false when ctx ends first. The grace lets the
// app reconnect, as it does when it restarts.
func (s *Service) WaitAppGone(ctx context.Context, first, grace time.Duration) bool {
	for {
		n, ever, changed := s.apps.state()
		if n > 0 {
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return false
			}
		}
		wait := first
		if ever {
			wait = grace
		}
		t := time.NewTimer(wait)
		select {
		case <-changed:
			t.Stop()
		case <-t.C:
			return true
		case <-ctx.Done():
			t.Stop()
			return false
		}
	}
}
