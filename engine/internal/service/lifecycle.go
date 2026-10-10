package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"coreshift/engine/internal/msg"
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
	return s.connectAtStart(ctx, "", "action.autostart")
}

// Resume connects the selected node again after a restart of the service
// interrupted a connection (Linux: a package upgrade restarts it), whatever
// the auto-connect setting says. Like AutoConnect it leaves alone a VPN the
// user connected or turned off meanwhile.
func (s *Service) Resume(ctx context.Context) error {
	return s.connectAtStart(ctx, "служба", "action.resume")
}

// LogAction notes in the journal why a connection is about to change, when
// the app's own buttons did not ask for it (they note it themselves): the
// user's action elsewhere, with source "" (Android's tile or notification,
// "Автозапуск"), or the service's own reason, with the journal's source
// for it ("служба", "обновление"). The app shows it as an "action" event.
// line goes as it is: LogActionMsg says it in the app's language.
func (s *Service) LogAction(source, line string) {
	s.hub.publish(Event{Kind: "action", Source: source, Line: line})
}

// LogActionMsg is LogAction for a sentence of CoreShift's own, internal/msg.
func (s *Service) LogActionMsg(source string, line msg.Msg) {
	s.hub.publish(Event{Kind: "action", Source: source}.withLine(line))
}

// SelectedName returns the name of the store's selected node, if there is
// a usable one.
func (s *Service) SelectedName() (string, bool) {
	if s.cfg.Store == nil {
		return "", false
	}
	_, n, ok := s.cfg.Store.Selected()
	return n.Name, ok
}

// connectAtStart connects the selected node when nothing is connected yet,
// retrying while the network comes up. Two things ask for it at the same
// start, each on its own goroutine: AutoConnect, and a self-update that
// interrupted a connection (finishAppUpdate). Both used to connect: the
// second Connect tore down the first connection a second after it came
// up, and made it again. Whichever comes first connects; the other
// returns nil, and so does a later one that finds the VPN already up.
// Each attempt is noted in the journal first (LogAction): why, the code of
// internal/msg, which with "_node" names the node too.
func (s *Service) connectAtStart(ctx context.Context, source, why string) error {
	if !s.startConn.TryLock() {
		return nil // the other one connects
	}
	defer s.startConn.Unlock()
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
		if name, ok := s.SelectedName(); ok {
			line := msg.New(why)
			if name != "" {
				line = msg.New(why+"_node", "name", name)
			}
			if i > 0 {
				line = msg.New("action.retry", "what", line)
			}
			s.LogActionMsg(source, line)
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

// WaitAppAttached returns true once an app is attached, at once if one is;
// false when ctx ends first. The Linux daemon runs from boot and follows
// the app with it: it connects when the app comes and disconnects when it
// goes, rather than starting and stopping with it as on Windows.
func (s *Service) WaitAppAttached(ctx context.Context) bool {
	for {
		n, _, changed := s.apps.state()
		if n > 0 {
			return true
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}
