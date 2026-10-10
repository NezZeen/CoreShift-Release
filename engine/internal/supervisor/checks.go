package supervisor

import (
	"context"
	"fmt"
	"time"
)

// awaitHealthy gives a fresh core up to Health.Failures attempts, a few
// seconds apart.
func (s *Supervisor) awaitHealthy(ctx context.Context, p *process) (time.Duration, error) {
	var err error
	for i := 0; i < s.cfg.Health.Failures; i++ {
		var lat time.Duration
		if lat, err = s.check(ctx, p); err == nil {
			return lat, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-p.Exited():
			return 0, p.ExitError()
		case <-p.lost():
			return 0, errPortTaken(p.listen)
		case <-time.After(min(s.cfg.Health.Interval, failRetry)):
		}
	}
	return 0, fmt.Errorf("health check failed: %w", err)
}

// check checks p's health and reports the result.
func (s *Supervisor) check(ctx context.Context, p *process) (time.Duration, error) {
	lat, err := s.checkHealth(ctx, p)
	s.report(ctx, p, lat, err)
	return lat, err
}

// checkHealth checks p's health through its own SOCKS inbound.
func (s *Supervisor) checkHealth(ctx context.Context, p *process) (time.Duration, error) {
	return checkHealth(ctx, p.auth.ProxyURL(p.listen), s.cfg.Health)
}

// report tells of a check of p.
func (s *Supervisor) report(ctx context.Context, p *process, lat time.Duration, err error) {
	// A check cut short by a disconnect, or by a switch to another server,
	// says nothing about the connection: reported, it read in the journal
	// as "the check failed: context canceled" on every switch.
	if err != nil && ctx.Err() != nil {
		return
	}
	s.emit(Event{Kind: EventHealth, Core: p.kind, Latency: lat, Err: err, Probe: p.probe})
}
