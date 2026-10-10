package service

import (
	"bufio"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// The pace of what wakes a phone, by its screen, its battery saver and
// «Экономия батареи» (the table in power.go).
func TestPowerPace(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.netPoll = netPollInterval })
	s := h.svc
	type pace struct{ traffic, net, resolvers time.Duration }
	look := func() pace { return pace{s.trafficEvery(), s.netEvery(), s.resolverEvery(0)} }
	cases := []struct {
		name          string
		screenOff     bool
		saver, noSave bool
		want          pace
	}{
		{"screen on", false, false, false, pace{time.Second, 2 * time.Second, 10 * time.Second}},
		{"screen on, battery saver", false, true, false, pace{2 * time.Second, 2 * time.Second, 10 * time.Second}},
		{"screen off", true, false, false, pace{30 * time.Second, 15 * time.Second, time.Minute}},
		{"screen off, battery saver", true, true, false, pace{2 * time.Minute, time.Minute, time.Minute}},
		// «Экономия батареи» off: the pace of a screen that is on, but the
		// traffic nobody sees.
		{"saving off, screen off", true, true, true, pace{30 * time.Second, 2 * time.Second, 10 * time.Second}},
		{"saving off, battery saver", false, true, true, pace{time.Second, 2 * time.Second, 10 * time.Second}},
	}
	for _, c := range cases {
		s.setBatterySaving(!c.noSave)
		s.SetPowerSave(c.saver)
		s.SetBackground(c.screenOff)
		if got := look(); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
		if s.idle() != (c.screenOff && !c.noSave) || s.saving() != (c.saver && !c.noSave) {
			t.Errorf("%s: idle %v, saving %v", c.name, s.idle(), s.saving())
		}
	}
	// A changed resolver seen once is confirmed at the usual pace.
	s.setBatterySaving(true)
	s.SetBackground(true)
	if d := s.resolverEvery(1); d != 10*time.Second {
		t.Errorf("confirming a change with the screen off: %v", d)
	}
}

// «Экономия батареи» comes from the settings and applies at once: no
// reconnect is asked for.
func TestBatterySavingOption(t *testing.T) {
	h := newHarness(t, nil)
	s := h.svc
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	s.SetBackground(true)
	if !s.idle() {
		t.Fatal("screen off, saving on by default: not idle")
	}
	o := s.Options()
	o.NoBatterySaving = true
	s.SetOptions(o)
	if s.idle() {
		t.Error("saving off: still idle")
	}
	if s.Status().Pending {
		t.Error("turning saving off asks for a reconnect")
	}
	o.NoBatterySaving = false
	s.SetOptions(o)
	if !s.idle() {
		t.Error("saving on again: not idle")
	}
}

// With the speed in a notification of its own (Android), a hidden app
// gets no traffic events, and the traffic keeps its pace; shown, the app
// has the speed at once.
func TestQuietViewGetsNoTraffic(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.TUN = false
		c.NotificationSpeed = true
		c.trafficEvery, c.trafficHiddenEvery = 50*time.Millisecond, time.Hour
	})
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = NewAPI(h.svc, token, netip.MustParseAddrPort(srv.Listener.Addr().String()))
	srv.Start()
	t.Cleanup(srv.Close)
	s := h.svc

	resp := viewStream(t, srv, "app=1&view=phone")
	traffic := make(chan struct{}, 1000)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") && strings.Contains(sc.Text(), `"kind":"traffic"`) {
				traffic <- struct{}{}
			}
		}
	}()
	waitUntil(t, "view open", func() bool { return s.SetViewHidden("phone", false) == nil })
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	next := func(within time.Duration) bool {
		select {
		case <-traffic:
			return true
		case <-time.After(within):
			return false
		}
	}
	if !next(2 * time.Second) {
		t.Fatal("no traffic while shown")
	}
	s.SetViewHidden("phone", true)
	if s.trafficEvery() != 50*time.Millisecond {
		t.Errorf("hidden: the traffic every %v, want the notification's pace", s.trafficEvery())
	}
	next(200 * time.Millisecond) // one under way
	for len(traffic) > 0 {
		<-traffic
	}
	if next(500 * time.Millisecond) {
		t.Error("traffic events to a hidden app")
	}
	s.SetViewHidden("phone", false)
	if !next(300 * time.Millisecond) {
		t.Error("no traffic at once after showing")
	}
}

// Without a notification of its own (the desktop), a hidden window still
// gets the speed, for the tray's tooltip.
func TestHiddenDesktopWindowIsNotQuiet(t *testing.T) {
	h := newHarness(t, nil)
	defer h.svc.AttachView("win")()
	if err := h.svc.SetViewHidden("win", true); err != nil {
		t.Fatal(err)
	}
	if h.svc.quietView("win") {
		t.Error("a hidden desktop window is quiet")
	}
}
