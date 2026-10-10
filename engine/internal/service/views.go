package service

import (
	"errors"
	"sync"
)

// views are the windows of the apps that follow the events (GET
// /v1/events?view=…), and whether each is hidden: in the tray or
// minimized on a desktop. While every one is hidden, nobody sees the speed
// and the traffic is sampled seldom (trafficHiddenInterval), only for the
// tray's tooltip and the day's totals. Unlike SetBackground (a phone's
// screen off), the device is in use meanwhile: the connection is checked
// as often as ever.
type views struct {
	mu sync.Mutex
	m  map[string]*view
}

type view struct {
	streams int // event streams open under the name; usually one
	hidden  bool
}

// ErrNoView means POST /v1/view named no open event stream.
var ErrNoView = errors.New("no event stream with this view")

// validView says whether id may name a view: what the app makes, 1 to 64
// letters, digits and dashes.
func validView(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// AttachView records an event stream of the view id, shown, until the
// returned func is called. A view opens shown: an app that restarted its
// stream says again if it is hidden.
func (s *Service) AttachView(id string) (detach func()) {
	v := &s.views
	v.mu.Lock()
	if v.m == nil {
		v.m = map[string]*view{}
	}
	w := v.m[id]
	if w == nil {
		w = &view{}
		v.m[id] = w
	}
	w.streams++
	w.hidden = false
	v.mu.Unlock()
	s.wakeTraffic()
	var once sync.Once
	return func() {
		once.Do(func() {
			v.mu.Lock()
			if w.streams--; w.streams == 0 {
				delete(v.m, id)
			}
			v.mu.Unlock()
		})
	}
}

// SetViewHidden says whether the window of view id is hidden. Shown again,
// the speed it shows is up to date at once.
func (s *Service) SetViewHidden(id string, hidden bool) error {
	v := &s.views
	v.mu.Lock()
	w := v.m[id]
	if w == nil {
		v.mu.Unlock()
		return ErrNoView
	}
	was := w.hidden
	w.hidden = hidden
	v.mu.Unlock()
	if was && !hidden {
		s.wakeTraffic()
	}
	return nil
}

// ViewsHidden reports whether there are views and all of them are hidden.
// Without any (an app of before 0.8.3, Android, no app at all) the traffic
// keeps its pace.
func (s *Service) ViewsHidden() bool {
	v := &s.views
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, w := range v.m {
		if !w.hidden {
			return false
		}
	}
	return len(v.m) > 0
}

// wakeTraffic has the traffic watcher sample now rather than at its next
// slow tick.
func (s *Service) wakeTraffic() {
	select {
	case s.awake <- struct{}{}:
	default:
	}
}

// quietView reports whether the event stream of view id may leave out
// what only a window on screen shows: the platform shows the speed itself
// (Config.NotificationSpeed, Android) and the view is hidden, which there
// means the app is in the background and draws nothing. Its traffic
// events and most pings are left out (api.go); shown again, the speed it
// shows is up to date at once (SetViewHidden).
func (s *Service) quietView(id string) bool {
	if !s.cfg.NotificationSpeed || id == "" {
		return false
	}
	v := &s.views
	v.mu.Lock()
	defer v.mu.Unlock()
	w := v.m[id]
	return w != nil && w.hidden
}
