package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// viewStream opens the event stream of view id; closing the body ends it.
func viewStream(t *testing.T, srv *httptest.Server, query string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/v1/events?"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: never happened", what)
		}
	}
}

// The traffic is sampled seldom only while every window is hidden; a
// window that shows again, or a new one, brings the pace back.
func TestHiddenViewsSlowTheTraffic(t *testing.T) {
	h, srv := newAPIServer(t)
	s := h.svc
	if s.ViewsHidden() || s.trafficEvery() != s.cfg.trafficEvery {
		t.Fatal("hidden without any window")
	}
	// A view nobody opened, or a name that cannot be one.
	if code, _ := call(t, srv, "POST", "/v1/view", `{"view":"nobody","hidden":true}`); code != http.StatusNotFound {
		t.Errorf("unknown view: %d", code)
	}
	bad := viewStream(t, srv, "view=not%20a%20name&app=1")
	waitUntil(t, "the stream open", func() bool { _, ever, _ := s.apps.state(); return ever })
	if code, _ := call(t, srv, "POST", "/v1/view", `{"view":"not a name","hidden":true}`); code != http.StatusNotFound {
		t.Errorf("invalid view name: %d", code)
	}
	bad.Body.Close()

	viewStream(t, srv, "replay=1&app=1&view=win-a")
	waitUntil(t, "view a open", func() bool { return s.SetViewHidden("win-a", false) == nil })
	if code, _ := call(t, srv, "POST", "/v1/view", `{"view":"win-a","hidden":true}`); code != http.StatusNoContent {
		t.Fatalf("hide: %d", code)
	}
	if !s.ViewsHidden() || s.trafficEvery() != s.cfg.trafficHiddenEvery {
		t.Errorf("one window, hidden: %v, every %v", s.ViewsHidden(), s.trafficEvery())
	}
	// Another user's window, shown: the speed is seen there.
	b := viewStream(t, srv, "app=1&view=win-b")
	waitUntil(t, "view b open", func() bool { return !s.ViewsHidden() })
	if s.trafficEvery() != s.cfg.trafficEvery {
		t.Errorf("a window shown: every %v", s.trafficEvery())
	}
	b.Body.Close()
	waitUntil(t, "view b gone", s.ViewsHidden)

	// Shown again: at once, the watcher woken.
	select {
	case <-s.awake:
	default:
	}
	if code, _ := call(t, srv, "POST", "/v1/view", `{"view":"win-a","hidden":false}`); code != http.StatusNoContent {
		t.Fatalf("show: %d", code)
	}
	select {
	case <-s.awake:
	default:
		t.Error("the traffic watcher was not woken")
	}
	if s.ViewsHidden() {
		t.Error("hidden after showing")
	}
	// The phone's screen off still wins.
	s.SetBackground(true)
	if s.trafficEvery() != s.cfg.trafficIdleEvery {
		t.Errorf("screen off: every %v", s.trafficEvery())
	}
	s.SetBackground(false)
}

// Without a token the view cannot be changed: the endpoint is behind the
// same checks as the rest of the API.
func TestViewNeedsTheToken(t *testing.T) {
	h, srv := newAPIServer(t)
	viewStream(t, srv, "app=1&view=win")
	waitUntil(t, "view open", func() bool { return h.svc.SetViewHidden("win", false) == nil })
	code, _ := call(t, srv, "POST", "/v1/view", `{"view":"win","hidden":true}`, func(r *http.Request) { r.Header.Del("Authorization") })
	if code != http.StatusUnauthorized || h.svc.ViewsHidden() {
		t.Errorf("without a token: %d, hidden %v", code, h.svc.ViewsHidden())
	}
}

// While hidden the traffic events come at the slow pace, and showing the
// window brings one at once.
func TestTrafficPaceFollowsTheWindow(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.trafficEvery, c.trafficHiddenEvery = 50*time.Millisecond, time.Hour })
	detach := h.svc.AttachView("w")
	defer detach()
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	next := func(within time.Duration) bool {
		end := time.After(within)
		for {
			select {
			case e := <-h.events:
				if e.Kind == kindTraffic {
					return true
				}
			case <-end:
				return false
			}
		}
	}
	if !next(2 * time.Second) {
		t.Fatal("no traffic while shown")
	}
	h.svc.SetViewHidden("w", true)
	next(200 * time.Millisecond) // a sample under way
	if next(500 * time.Millisecond) {
		t.Error("traffic events at the shown pace while hidden")
	}
	h.svc.SetViewHidden("w", false)
	if !next(300 * time.Millisecond) {
		t.Error("no traffic at once after showing")
	}
}
