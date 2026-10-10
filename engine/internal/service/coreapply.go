package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/dnsguard"
	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/supervisor"
	"coreshift/engine/internal/tunlayer"
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
	u := staleCore{version: v, installed: time.Now()}
	s.cores.stale[k] = u
	if k == core.SingBox {
		// Whichever core runs, on the desktop the TUN layer is sing-box.
		s.cores.tun = &u
	}
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

// staleTUN returns the sing-box update the TUN layer may not run yet.
func (s *Service) staleTUN() (staleCore, bool) {
	s.cores.mu.Lock()
	defer s.cores.mu.Unlock()
	if s.cores.tun == nil {
		return staleCore{}, false
	}
	return *s.cores.tun, true
}

func (s *Service) forgetStaleTUN(u staleCore) {
	s.cores.mu.Lock()
	defer s.cores.mu.Unlock()
	if s.cores.tun != nil && *s.cores.tun == u {
		s.cores.tun = nil
	}
}

// applyCores restarts connection gen's core onto its updated executable
// once the connection is quiet (coreApplyQuiet), and tells so with a
// "cores" event, Reason "applied"; after it, in the same quiet moment, the
// desktop's TUN layer onto an updated sing-box (applyTUN). It ends with
// the connection, or when neither runs a version since replaced.
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
		tu, tunOK := s.staleTUN()
		if !ok && !tunOK {
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
		if ok && !s.applyCore(k, u, &fails) {
			// The TUN layer waits for the core: one interruption at a
			// time, and none on top of a core that did not move.
			continue
		}
		if tunOK {
			s.applyTUN(gen, tu)
		}
	}
}

// applyCore restarts the running core k onto update u, and reports whether
// it runs that version now.
func (s *Service) applyCore(k core.Kind, u staleCore, fails *int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), coreApplyTimeout)
	err := s.sup.Restart(ctx, k, u.installed)
	cancel()
	switch {
	case err == nil:
		s.forgetStale(k, u)
		s.hub.publish(Event{Kind: "cores", Core: string(k), Reason: "applied", Line: u.version})
		return true
	case errors.Is(err, supervisor.ErrNotNeeded):
		s.forgetStale(k, u)
		return true
	case errors.Is(err, supervisor.ErrNotConnected):
		// The connection ends: the next one starts the new executable.
	default:
		if *fails++; *fails >= coreApplyTries {
			s.forgetStale(k, u)
		}
	}
	return false
}

// applyTUN starts connection gen's TUN layer again on the updated sing-box
// executable, with the options it runs with, and tells so with a "tun"
// event, Reason "updated", Line the version. The interface goes for the
// second or two the new start takes; the connection stays, and the system
// DNS stays pointed into the tunnel throughout (a guard bound to the
// interface is applied to the new one, dnsguard.LinkBound). Under s.op, so
// it never races a connect or a disconnect, which cancels it.
//
// What may go wrong is tried before the interface is touched: the new
// version validates the config first, and a refusal leaves the layer as it
// is, on the old version until the next connection. A new start that still
// fails falls back to the version the update replaced; failing too, the
// connection is made anew, as after any TUN layer that died.
func (s *Service) applyTUN(gen int, u staleCore) {
	ctx, end := s.beginOp(context.Background())
	defer end()
	s.mu.Lock()
	n, current := s.lastNode, gen == s.gen && s.status.State == Connected
	s.mu.Unlock()
	up, ok := s.cfg.tun.(tunUpdatable)
	if !current || disconnected(ctx) {
		return // the next connection starts the new executable
	}
	if !ok || s.tun == nil || s.tunAt.After(u.installed) {
		// No TUN layer, Android's (in the app), or one started on the new
		// version already.
		s.forgetStaleTUN(u)
		return
	}
	s.forgetStaleTUN(u)
	tctx, cancel := context.WithTimeout(ctx, coreApplyTimeout)
	defer cancel()
	opts := s.tunOpts
	if err := up.Check(tctx, opts); err != nil {
		if !disconnected(ctx) {
			s.hub.publish(Event{Kind: "tun"}.withError(msg.New("tun.update_refused", "version", u.version, "err", msg.Raw(err.Error()))))
		}
		return
	}
	// Stopped on purpose: its watchTUN must not take the connection down.
	s.tunSeq++
	s.tun.Stop()
	s.tun = nil
	started := time.Now()
	inst, err := up.Start(tctx, opts)
	if err == nil {
		if s.tunBack(ctx, inst, gen, opts, started) {
			s.hub.publish(Event{Kind: "tun", Reason: "updated", Core: string(core.SingBox), Line: u.version})
		}
		return
	}
	if disconnected(ctx) {
		return // Disconnect finishes what is left
	}
	started = time.Now()
	if prev, perr := up.StartPrevious(tctx, opts); perr == nil {
		if s.tunBack(ctx, prev, gen, opts, started) {
			s.hub.publish(Event{Kind: "tun"}.withError(msg.New("tun.update_rollback", "version", u.version, "err", msg.Raw(err.Error()))))
		}
		return
	} else if disconnected(ctx) {
		return
	}
	s.hub.publish(Event{Kind: "tun"}.withError(msg.New("tun.update_reconnect", "version", u.version, "err", msg.Raw(err.Error()))))
	_ = s.connectOp(ctx, n)
}

// tunBack makes inst connection gen's TUN layer after applyTUN stopped the
// previous one, and reports whether the connection goes on with it.
func (s *Service) tunBack(ctx context.Context, inst TUNInstance, gen int, o tunlayer.Options, started time.Time) bool {
	s.tunUp(inst, gen, o, started)
	if _, ok := s.cfg.guard.(dnsguard.LinkBound); ok {
		if err := s.cfg.guard.Apply(ctx, s.guardCfg); err != nil {
			s.stopLocked()
			if !disconnected(ctx) {
				s.fail(fmt.Errorf("redirect system DNS: %w", err))
			}
			return false
		}
	}
	return true
}
