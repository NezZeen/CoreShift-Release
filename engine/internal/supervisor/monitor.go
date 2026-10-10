package supervisor

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
)

// SetIdle says whether the device is idle: a phone with its screen off.
// Meanwhile a healthy connection is checked only every IdleHealthInterval:
// each check is a request through the server, which keeps a phone's radio
// awake. A check that fails is repeated as soon as ever, so a server that
// stops answering is still left as quickly once that is seen; and when
// the device is no longer idle the connection is checked at once.
func (s *Supervisor) SetIdle(idle bool) {
	if s.idle.Swap(idle) == idle || idle {
		return
	}
	select {
	case s.awake <- struct{}{}:
	default:
	}
}

// healthEvery is how long after a check that passed the next one comes.
func (s *Supervisor) healthEvery() time.Duration {
	d := max(s.cfg.Health.Interval, time.Duration(s.healthFloor.Load()))
	if s.idle.Load() {
		return max(d, s.cfg.IdleHealthInterval)
	}
	return d
}

// A core whose local port does not answer hungChecks failed checks in a
// row is restarted; one that hangs again within hungRestartWindow is
// dropped for the next core.
const (
	hungChecks        = 2
	hungRestartWindow = 10 * time.Minute
	portDialTimeout   = 2 * time.Second
)

// portAnswers reports whether something accepts connections at addr. A
// check cancelled from outside counts as an answer: it proves nothing.
func portAnswers(ctx context.Context, addr netip.AddrPort) bool {
	dctx, cancel := context.WithTimeout(ctx, portDialTimeout)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(dctx, "tcp", addr.String())
	if err != nil {
		return ctx.Err() != nil
	}
	c.Close()
	return true
}

// failRetry is how soon a failed health check is repeated: often enough to
// notice quickly whether the connection works or another core should take
// over.
const failRetry = 3 * time.Second

// startRetry is how soon a fresh core's first check is repeated when it
// failed. That check runs the moment the core takes connections, while the
// service is still bringing the TUN layer up and redirecting DNS; on
// Android the VPN coming up moves the phone's default network under the
// core, which drops the connection it just opened to the server. The check
// then fails with "EOF" a second before the next passes: no news, and the
// journal said "the check failed" on nearly every connection. The first
// failure is retried quietly once; a second one is reported and counted.
const startRetry = time.Second

// monitor watches the running core and returns why it must be replaced, or
// "" when ctx is cancelled. For ReasonHealth it also returns the core that
// passed its check on the spare port.
func (s *Supervisor) monitor(ctx context.Context, p *process, n node.Node, serverAddr string, chain []core.Kind, failed map[core.Kind]error) (Reason, core.Kind, error) {
	primary := chain[0]
	check := time.NewTimer(0) // the first check right away
	defer check.Stop()
	var back <-chan time.Time
	returnAfter := s.cfg.ReturnToPrimaryAfter
	if s.cfg.Mode == Auto && p.kind != primary && returnAfter > 0 {
		back = time.After(returnAfter)
	}
	// Looking for a working core starts several; after a search that found
	// none, the next waits a while.
	searchEvery := 10 * s.cfg.Health.Interval
	var searched time.Time
	fails, deaf := 0, 0
	starting := true // the core's first check is yet to pass or fail twice (startRetry)
	for {
		select {
		case <-ctx.Done():
			return "", "", nil
		case <-p.Exited():
			return ReasonExited, "", p.ExitError()
		case <-p.lost():
			// Its port answers, but not with this core behind it.
			return ReasonExited, "", errPortTaken(p.listen)
		case <-check.C:
			lat, err := s.checkHealth(ctx, p)
			if ctx.Err() != nil {
				return "", "", nil
			}
			if err != nil && starting {
				// Neither reported nor counted: see startRetry.
				starting = false
				check.Reset(min(s.healthEvery(), startRetry))
				continue
			}
			starting = false
			s.report(ctx, p, lat, err)
			delay := s.healthEvery()
			switch {
			case err == nil:
				fails, deaf = 0, 0
			case s.offline():
				// Without a network every check fails, whatever the core:
				// swapping cores or restarting this one would not help, and
				// counting these failures would swap right after the network
				// returns. Checked often, to see it return; with the screen
				// off at the idle pace, as the service watches the network
				// itself.
				fails, deaf = 0, 0
				if !s.idle.Load() {
					delay = min(delay, failRetry)
				}
			default:
				fails++
				delay = min(delay, failRetry)
				// A core that no longer takes connections on its own port
				// is hung: more checks, or another server, would not help.
				if portAnswers(ctx, p.listen) {
					deaf = 0
				} else if deaf++; deaf >= hungChecks {
					return ReasonHung, "", fmt.Errorf("the core stopped taking connections on %s; last check: %w", p.listen, err)
				}
				if fails >= s.cfg.Health.Failures && time.Since(searched) >= searchEvery {
					searched = time.Now()
					// With a core chosen by hand there is no other to try.
					if s.cfg.Mode == Auto {
						if alt, ok := s.findWorking(ctx, chain, failed, p.kind, n, serverAddr); ok {
							return ReasonHealth, alt, fmt.Errorf("%d health checks in a row failed, last: %w", fails, err)
						}
						if ctx.Err() != nil {
							return "", "", nil
						}
					}
					// No core does better: the connection stays. Unless the
					// network went away during the search, which explains it.
					if !s.offline() {
						s.emit(Event{Kind: EventNoBetter, Core: p.kind, Err: err})
					}
				}
			}
			check.Reset(delay)
		case <-s.awake:
			// The device is in use again: whatever happened to the server
			// meanwhile is seen now, not a minute later.
			check.Reset(0)
			back = backAfterIdle(back)
		case <-back:
			if s.idle.Load() {
				// Starting the primary aside waits for the screen (power.go).
				back = deferredBack
				continue
			}
			if s.probe(ctx, primary, n, serverAddr) == nil {
				return ReasonReturn, "", nil
			}
			back = time.After(returnAfter)
		case reply := <-s.returnReq:
			if p.kind == primary {
				reply <- ErrOnPrimary
				continue
			}
			err := s.probe(ctx, primary, n, serverAddr)
			reply <- err
			if err == nil {
				return ReasonReturn, "", nil
			}
		case req := <-s.restartReq:
			if p.kind != req.kind || !p.started.Before(req.before) {
				req.reply <- ErrNotNeeded
				continue
			}
			// The new executable is tried aside first: one that does not
			// work here must not take the connection down.
			if err := s.probe(ctx, p.kind, n, serverAddr); err != nil {
				if ctx.Err() != nil {
					req.reply <- ErrNotConnected
					return "", "", nil
				}
				req.reply <- err
				continue
			}
			s.restarted = req.reply
			return ReasonRestart, "", nil
		}
	}
}

// findWorking tries the chain's other cores on the spare port, in order,
// and returns the first that passes its health check.
func (s *Supervisor) findWorking(ctx context.Context, chain []core.Kind, failed map[core.Kind]error, current core.Kind, n node.Node, serverAddr string) (core.Kind, bool) {
	for _, k := range chain {
		if k == current || failed[k] != nil {
			continue
		}
		if s.probe(ctx, k, n, serverAddr) == nil {
			return k, true
		}
		if ctx.Err() != nil {
			break
		}
	}
	return "", false
}

// probe starts k on a spare port and reports whether it becomes healthy.
func (s *Supervisor) probe(ctx context.Context, k core.Kind, n node.Node, serverAddr string) error {
	p, err := s.launch(ctx, k, n, serverAddr, true)
	if err == nil {
		_, err = s.awaitHealthy(ctx, p)
	}
	p.stop()
	return err
}

// offline reports what Config.Offline says, false without it.
func (s *Supervisor) offline() bool { return s.cfg.Offline != nil && s.cfg.Offline() }
