package service

import (
	"sync"
	"sync/atomic"
	"time"
)

// Saving a phone's battery (Android). The app tells the engine whether the
// screen is on (SetBackground) and whether the phone saves its battery
// (SetPowerSave: battery saver on, or Doze). While the user's «Экономия
// батареи» is on (the default; Options.NoBatterySaving off), they slow
// down what wakes the phone and its radio:
//
//	                    traffic  healthy check     network  resolvers
//	screen on           1 s      Health.Interval   2 s      10 s
//	  + battery saver   2 s      at least 30 s     2 s      10 s
//	screen off          30 s     at least 1 min    15 s     1 min
//	  + battery saver   2 min    at least 5 min    1 min    1 min
//
// What keeps the connection right does not slow down: a check that failed
// is repeated within seconds, so a dead server is left as quickly once a
// check sees it; Android tells of a network that came or went at once
// (NetworkChanged); a changed resolver is confirmed at the usual pace once
// seen; and when the screen comes on, the connection is checked, the
// network looked at and the traffic sampled at once. With the screen off
// the core is not tried aside for a return to the primary either: that
// waits for the screen (supervisor.SetHealthFloor, SetIdle).
//
// With «Экономия батареи» off everything keeps the pace of a screen that
// is on, but the traffic: nobody sees it with the screen off, so it is
// sampled every half minute then, for the day's totals, whatever the
// setting.
const (
	// trafficSaverInterval: with the screen on in battery saver, the
	// notification's speed moves every other second.
	trafficSaverInterval = 2 * time.Second
	// trafficIdleSaverInterval: with the screen off in battery saver only
	// the day's totals need the traffic.
	trafficIdleSaverInterval = 2 * time.Minute
	// healthSaverFloor and healthIdleSaverFloor are the least time between
	// checks of a healthy connection in battery saver, with the screen on
	// and off.
	healthSaverFloor     = 30 * time.Second
	healthIdleSaverFloor = 5 * time.Minute
	// netPollIdleSaver is netPollIdle in battery saver.
	netPollIdleSaver = time.Minute
	// resolverIdleInterval is how often, with the screen off, a connection
	// that took the system's resolver looks whether it is still there
	// (watchNetwork); networkCheckInterval otherwise.
	resolverIdleInterval = time.Minute
)

// powerState is what the device and the user said about saving power.
type powerState struct {
	mu    sync.Mutex  // one applyPower at a time
	saver atomic.Bool // the phone saves its battery (SetPowerSave)
	off   atomic.Bool // «Экономия батареи» is off (Options.NoBatterySaving)
}

// SetBackground says whether the device is idle: a phone with its screen
// off, where no one sees the speed and every wakeup costs battery. See
// the table above for what slows down meanwhile. Back in use, the
// connection is checked, the network looked at and the traffic sampled at
// once.
func (s *Service) SetBackground(bg bool) {
	if s.bg.Swap(bg) == bg {
		return
	}
	s.applyPower()
	if !bg {
		s.wakeTraffic()
		s.kickNetwork() // the network watcher too (netwatch.go)
	}
}

// Background reports what SetBackground set last: whether the screen is
// off, whatever «Экономия батареи» says.
func (s *Service) Background() bool { return s.bg.Load() }

// SetPowerSave says whether the phone saves its battery: Android's battery
// saver is on, or the phone dozes. See the table above.
func (s *Service) SetPowerSave(on bool) {
	if s.power.saver.Swap(on) == on {
		return
	}
	s.applyPower()
}

// setBatterySaving follows «Экономия батареи» (Options.NoBatterySaving).
func (s *Service) setBatterySaving(on bool) {
	if s.power.off.Swap(!on) == !on {
		return
	}
	s.applyPower()
}

// idle reports whether the engine may act as on an idle device: the
// screen is off and «Экономия батареи» on.
func (s *Service) idle() bool { return s.bg.Load() && !s.power.off.Load() }

// saving reports whether the phone saves its battery and the engine may
// help it: battery saver or Doze, and «Экономия батареи» on.
func (s *Service) saving() bool { return s.power.saver.Load() && !s.power.off.Load() }

// applyPower passes the pace of the checks on to the supervisor.
func (s *Service) applyPower() {
	s.power.mu.Lock()
	defer s.power.mu.Unlock()
	idle := s.idle()
	var floor time.Duration
	if s.saving() {
		floor = healthSaverFloor
		if idle {
			floor = healthIdleSaverFloor
		}
	}
	// The floor first: a device in use again is checked at once, at the
	// new pace.
	s.sup.SetHealthFloor(floor)
	s.sup.SetIdle(idle)
}

// trafficEvery is how long until the traffic is sampled next.
func (s *Service) trafficEvery() time.Duration {
	switch {
	case s.bg.Load() && s.saving():
		return max(s.cfg.trafficIdleEvery, trafficIdleSaverInterval)
	case s.bg.Load():
		return s.cfg.trafficIdleEvery
	case s.ViewsHidden() && !s.cfg.NotificationSpeed:
		// Android's notification shows the speed whatever the app's
		// window does.
		return s.cfg.trafficHiddenEvery
	case s.saving():
		return max(s.cfg.trafficEvery, trafficSaverInterval)
	}
	return s.cfg.trafficEvery
}

// netEvery is how long until the network watcher looks next.
func (s *Service) netEvery() time.Duration {
	switch {
	case s.idle() && s.saving():
		return max(s.cfg.netPoll, netPollIdleSaver)
	case s.idle():
		return max(s.cfg.netPoll, netPollIdle)
	}
	return s.cfg.netPoll
}

// resolverEvery is how long until watchNetwork looks at the system's
// resolvers next; seen once is the change confirmed at the usual pace.
func (s *Service) resolverEvery(seen int) time.Duration {
	if s.idle() && seen == 0 {
		return max(s.cfg.netInterval, resolverIdleInterval)
	}
	return s.cfg.netInterval
}
