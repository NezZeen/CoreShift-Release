package service

import (
	"context"
	"time"

	"coreshift/engine/internal/core"
)

const trafficInterval = time.Second

// watchTraffic publishes the node's traffic every second while connected:
// totals for the connection, which may span several cores after swaps,
// and the rate since the last sample.
func (s *Service) watchTraffic(ctx context.Context) {
	tick := time.NewTicker(trafficInterval)
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
		}
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
