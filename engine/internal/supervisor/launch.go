package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/proc"
)

// launchAttempts is how many random ports a core is started on before a
// port taken under it counts as its failure. Another program would have
// to guess the port in the moment between choosing it and the core
// opening it.
const launchAttempts = 3

// launch starts k on a random loopback port of its own: the running core
// behind Listen, or a probe.
func (s *Supervisor) launch(ctx context.Context, k core.Kind, n node.Node, serverAddr string, probe bool) (*process, error) {
	var err error
	for range launchAttempts {
		var listen netip.AddrPort
		if listen, err = freeLoopbackPort(); err != nil {
			return nil, err
		}
		var p *process
		p, err = s.launchOn(ctx, k, n, serverAddr, listen, probe)
		if err == nil || !errors.Is(err, errTaken) || ctx.Err() != nil {
			return p, err
		}
	}
	return nil, err
}

// launchOn writes the config for k and starts it listening on listen.
func (s *Supervisor) launchOn(ctx context.Context, k core.Kind, n node.Node, serverAddr string, listen netip.AddrPort, probe bool) (*process, error) {
	a, _ := core.ByKind(k)
	dir := filepath.Join(s.cfg.WorkDir, string(k))
	if probe {
		dir += "-probe"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Credentials of its own, new for every start: no client of Listen,
	// nor any other program, is given them.
	auth := core.NewSOCKSAuth()
	o := core.Options{Listen: listen, Auth: auth, LogLevel: s.cfg.LogLevel, ServerAddr: serverAddr, Fragment: s.cfg.Fragment}
	cfg, err := a.Render(&n, o)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, a.ConfigName())
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return nil, err
	}
	if proc.PortOpen(listen) {
		return nil, errPortTaken(listen)
	}

	watch := newPortWatch(listen)
	started := time.Now()
	pr, err := s.group.Start(proc.Spec{
		Name: string(k),
		Path: s.cfg.Binaries[k],
		Args: a.RunArgs(path, dir),
		Dir:  dir,
		OnLine: func(line string) {
			watch.line(line)
			s.emit(Event{Kind: EventLog, Core: k, Line: line, Probe: probe})
		},
	})
	if err != nil {
		return nil, err
	}
	p := &process{Process: pr, kind: k, listen: listen, auth: auth, probe: probe, portLost: watch.ch, started: started}
	// A core that could not open its port is done waiting for: the port
	// answering then is someone else's.
	ready := func() bool { return watch.failed() || proc.PortOpen(listen) }
	if err := pr.WaitFor(ctx, s.cfg.StartTimeout, "socks port "+listen.String(), ready); err != nil {
		p.stop()
		return nil, err
	}
	if watch.failed() || !coreListens(pr, listen) {
		p.stop()
		return nil, errPortTaken(listen)
	}
	return p, nil
}

// coreListens reports whether the port that answers at listen is pr's own:
// another program could have taken it in the moment between choosing it
// and the core opening it, and would then receive what is sent there.
// Where the system does not tell (Android), the port is taken for the
// core's, being random and open to guessing only in that moment.
func coreListens(pr *proc.Process, listen netip.AddrPort) bool {
	owned, err := proc.Listens(pr.Pid(), listen)
	return owned || err != nil
}

// errTaken is what errPortTaken wraps.
var errTaken = errors.New("another program holds it")

// errPortTaken is a core's failure to open its SOCKS port at addr.
func errPortTaken(addr netip.AddrPort) error {
	return fmt.Errorf("the core could not open its port %s: %w", addr, errTaken)
}
