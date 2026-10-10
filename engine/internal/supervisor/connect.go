package supervisor

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/proc"
	"coreshift/engine/internal/socksgate"
)

// Connect starts serving n and returns once a core runs, or with an error
// when none can start. serverAddr, if not empty, is the already
// resolved address of n's server (see core.Options.ServerAddr). After a
// successful return the supervisor keeps watching and swapping in the
// background until Disconnect.
func (s *Supervisor) Connect(ctx context.Context, n node.Node, serverAddr string) error {
	s.Disconnect()
	chain, err := s.chain(&n)
	if err != nil {
		return err
	}
	gate, err := socksgate.Listen(s.cfg.gateConfig())
	if err != nil {
		return portError(s.cfg.Listen, err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	ready := make(chan error, 1)
	done := make(chan struct{})
	s.mu.Lock()
	s.cancel, s.done, s.gate = cancel, done, gate
	s.gates++
	s.status = Status{State: Connecting, Node: n.Name, Chain: chain, Failed: map[core.Kind]string{}, Since: time.Now()}
	s.mu.Unlock()

	go func() {
		defer close(done)
		s.run(runCtx, n, serverAddr, chain, ready)
	}()
	select {
	case err := <-ready:
		if err != nil {
			s.Disconnect() // gives the port back
		}
		return err
	case <-ctx.Done():
		s.Disconnect()
		return ctx.Err()
	}
}

// portError explains why the SOCKS port at addr could not be opened.
func portError(addr netip.AddrPort, err error) error {
	if proc.PortOpen(addr) {
		return fmt.Errorf("listen address %s is already in use", addr)
	}
	return fmt.Errorf("open the SOCKS port: %w", err)
}

// SOCKSAuth returns the credentials Listen requires (Config.Auth).
func (s *Supervisor) SOCKSAuth() core.SOCKSAuth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Auth
}

// Disconnect stops the running core and frees Listen. It is a no-op when
// idle.
func (s *Supervisor) Disconnect() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	s.mu.Lock()
	gate := s.gate
	s.gate = nil
	s.mu.Unlock()
	if gate != nil {
		gate.Close()
	}
}

// CoreListen is the running core's own SOCKS port, which changes with
// every start; false when no core runs.
func (s *Supervisor) CoreListen() (netip.AddrPort, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return netip.AddrPort{}, false
	}
	return s.active.listen, true
}

// serve makes p the core behind Listen; nil for none.
func (s *Supervisor) serve(p *process) {
	s.mu.Lock()
	gate := s.gate
	s.active = p
	s.mu.Unlock()
	if gate == nil {
		return
	}
	t := socksgate.Target{}
	if p != nil {
		t = socksgate.Target{Addr: p.listen, Auth: p.auth}
	}
	gate.SetTarget(t)
}
