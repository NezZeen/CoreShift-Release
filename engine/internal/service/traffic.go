package service

import (
	"context"
	"time"

	"coreshift/engine/internal/core"
)

const (
	trafficInterval = time.Second
	// trafficIdleInterval is how often the traffic is sampled while the
	// device is idle (SetBackground): only the day's totals need it then.
	trafficIdleInterval = 30 * time.Second
)

// SetBackground says whether the device is idle: a phone with its screen
// off, where no one sees the speed and every wakeup costs battery. The
// traffic is then sampled every half minute rather than every second, and
// a healthy connection is checked once a minute (supervisor.SetIdle); a
// failing one as often as ever. Back in use, both catch up at once.
func (s *Service) SetBackground(bg bool) {
	if s.bg.Swap(bg) == bg {
		return
	}
	s.sup.SetIdle(bg)
	if !bg {
		select {
		case s.awake <- struct{}{}:
		default:
		}
	}
}

// Background reports what SetBackground set last.
func (s *Service) Background() bool { return s.bg.Load() }

func (s *Service) trafficEvery() time.Duration {
	if s.bg.Load() {
		return s.cfg.trafficIdleEvery
	}
	return s.cfg.trafficEvery
}

// watchTraffic publishes the node's traffic every second while connected
// (every trafficIdleInterval while the device is idle): totals for the
// connection, which may span several cores after swaps, and the rate
// since the last sample.
func (s *Service) watchTraffic(ctx context.Context) {
	// A wakeup from before this connection is no news to it.
	select {
	case <-s.awake:
	default:
	}
	tick := time.NewTimer(s.trafficEvery())
	defer tick.Stop()
	// The day's totals reach the disk now and then, and when the connection ends.
	defer func() { s.stats.flush(time.Now()) }()
	lastFlush := time.Now()
	var total, last core.Traffic
	lastRun := -1
	lastAt := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.awake:
			// The screen came on: the speed it shows is up to date at once.
		}
		tick.Reset(s.trafficEvery())
		now, run, err := s.sup.Traffic(ctx)
		if err != nil {
			continue // swapping, or a core without counters; the next sample makes up
		}
		if run != lastRun {
			// A new core counts from zero.
			last, lastRun = core.Traffic{}, run
		}
		d := core.Traffic{Up: now.Up - last.Up, Down: now.Down - last.Down}
		if d.Up < 0 || d.Down < 0 {
			d = now
		}
		last = now
		s.stats.add(time.Now(), d.Up, d.Down)
		if time.Since(lastFlush) >= statsFlushEvery {
			s.stats.flush(time.Now())
			lastFlush = time.Now()
		}
		total.Up += d.Up
		total.Down += d.Down
		at := time.Now()
		secs := at.Sub(lastAt).Seconds()
		lastAt = at
		if secs <= 0 {
			secs = 1
		}
		s.hub.publish(Event{
			Kind: kindTraffic, Up: total.Up, Down: total.Down,
			UpRate: int64(float64(d.Up) / secs), DownRate: int64(float64(d.Down) / secs),
		})
	}
}
