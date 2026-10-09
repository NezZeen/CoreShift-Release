package service

import (
	"context"
	"errors"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/supervisor"
)

// A core updated while connected is applied without disconnecting: once
// the connection has carried next to nothing for a while, the running core
// is started again from its new executable (supervisor.Restart), as after
// a swap: the SOCKS port stays the service's, and connections arriving
// meanwhile wait for the new start. A connection always busy keeps the old
// version until it is started again anyway: a reconnect, a change of
// network or server, a failover.
const (
	// coreApplyEvery is how often the traffic is looked at.
	coreApplyEvery = 5 * time.Second
	// coreApplyQuiet is how long the connection must stay below
	// coreApplyRate: a download or a call is not cut.
	coreApplyQuiet = time.Minute
	// coreApplyRate is bytes a second, both ways together: below a voice
	// call, above what programs exchange on their own (keep-alives,
	// messengers waiting for messages).
	coreApplyRate = 2 << 10
	// coreApplyTries is how many times a new start that does not work
	// here is tried before the update is left for the next connection.
	coreApplyTries = 3
	// coreApplyTimeout bounds one restart: trying the new start aside,
	// then the swap.
	coreApplyTimeout = 2 * time.Minute
)

// staleCore is an update installed while its core may be running.
type staleCore struct {
	version   string
	installed time.Time
}

// noteInstalled records that k's executable was replaced by version v
// while a connection may be running it, and starts applying it to the
// connection (applyCores).
func (s *Service) noteInstalled(k core.Kind, v string) {
	s.mu.Lock()
	gen, active := s.gen, s.status.State.active()
	s.mu.Unlock()
	if !active {
		return
	}
	s.cores.mu.Lock()
	defer s.cores.mu.Unlock()
	if s.cores.stale == nil {
		s.cores.stale = map[core.Kind]staleCore{}
	}
	s.cores.stale[k] = staleCore{version: v, installed: time.Now()}
	if s.cores.applying != gen {
		s.cores.applying = gen
		go s.applyCores(gen)
	}
}

// staleActive returns the update of the running core, if there is one;
// updates of the other cores are forgotten, since they do not run: their
// next start runs the new executable.
func (s *Service) staleActive() (core.Kind, staleCore, bool) {
	active := s.sup.Status().Core
	s.cores.mu.Lock()
	defer s.cores.mu.Unlock()
	for k := range s.cores.stale {
		if k != active {
			delete(s.cores.stale, k)
		}
	}
	u, ok := s.cores.stale[active]
	return active, u, ok
}

func (s *Service) forgetStale(k core.Kind, u staleCore) {
	s.cores.mu.Lock()
	defer s.cores.mu.Unlock()
	if s.cores.stale[k] == u { // not a newer update meanwhile
		delete(s.cores.stale, k)
	}
}

// applyCores restarts connection gen's core onto its updated executable
// once the connection is quiet (coreApplyQuiet), and tells so with a
// "cores" event, Reason "applied". It ends with the connection, or when
// the running core is not one updated.
func (s *Service) applyCores(gen int) {
	defer func() {
		s.cores.mu.Lock()
		if s.cores.applying == gen {
			s.cores.applying = 0
		}
		s.cores.mu.Unlock()
	}()
	tick := time.NewTicker(s.cfg.coreApplyEvery)
	defer tick.Stop()
	var last core.Traffic
	var lastAt time.Time
	lastRun := -1
	var quiet time.Duration
	fails := 0
	for range tick.C {
		s.mu.Lock()
		st, cur := s.status.State, s.gen
		s.mu.Unlock()
		if cur != gen || !st.active() {
			return
		}
		if st != Connected || s.sup.Status().State != supervisor.Connected {
			// Coming up, held for want of a network, or swapping cores:
			// looked at again, the quiet counted afresh.
			lastRun, quiet = -1, 0
			continue
		}
		k, u, ok := s.staleActive()
		if !ok {
			return
		}
		t, run, err := s.sup.Traffic()
		now := time.Now()
		if err != nil || run != lastRun {
			last, lastAt, lastRun, quiet = t, now, run, 0
			continue
		}
		took := now.Sub(lastAt)
		moved := t.Up - last.Up + t.Down - last.Down
		last, lastAt = t, now
		if moved < 0 || float64(moved) > float64(s.cfg.coreApplyRate)*took.Seconds() {
			quiet = 0
			continue
		}
		if quiet += took; quiet < s.cfg.coreApplyQuiet {
			continue
		}
		quiet = 0
		ctx, cancel := context.WithTimeout(context.Background(), coreApplyTimeout)
		err = s.sup.Restart(ctx, k, u.installed)
		cancel()
		switch {
		case err == nil:
			s.forgetStale(k, u)
			s.hub.publish(Event{Kind: "cores", Core: string(k), Reason: "applied", Line: u.version})
		case errors.Is(err, supervisor.ErrNotNeeded):
			s.forgetStale(k, u)
		case errors.Is(err, supervisor.ErrNotConnected):
			// The connection ends: the next one starts the new executable.
		default:
			if fails++; fails >= coreApplyTries {
				s.forgetStale(k, u)
			}
		}
	}
}
