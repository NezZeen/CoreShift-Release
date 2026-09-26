// Package supervisor runs proxy cores and swaps between them.
//
// For a node it builds the chain of installed cores able to run it, in
// priority order, starts the first one and watches it. When the core fails to
// start, exits, or keeps failing health checks, the next core in the chain
// takes over on the same SOCKS port, so the TUN layer in front never notices
// more than a short gap. After running on a backup for a while it probes the
// primary core on a spare port and moves back once that works.
package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/proc"
)

type Mode string

const (
	// Auto swaps to the next compatible core on failure.
	Auto Mode = "auto"
	// Manual runs only Config.ManualCore and gives up when it fails.
	Manual Mode = "manual"
)

type State string

const (
	Idle       State = "idle"
	Connecting State = "connecting"
	Connected  State = "connected"
	Swapping   State = "swapping"
	Failed     State = "failed"
)

// Reason says why a core was left.
type Reason string

const (
	ReasonStartFailed Reason = "start-failed"
	ReasonExited      Reason = "exited"
	ReasonHealth      Reason = "health-check"
	ReasonReturn      Reason = "return-to-primary"
)

var (
	// ErrNoCore means no installed core supports the node.
	ErrNoCore = errors.New("no installed core supports this node")
	// ErrChainExhausted means every compatible core failed.
	ErrChainExhausted = errors.New("every compatible core failed")
	// ErrOnPrimary means ReturnToPrimary found the primary core running.
	ErrOnPrimary = errors.New("already on the primary core")
	// ErrNotConnected means there is no running core.
	ErrNotConnected = errors.New("not connected")
)

type Health struct {
	URL      string
	Interval time.Duration
	Timeout  time.Duration
	// Failures is how many checks in a row must fail before swapping.
	Failures int
	// MaxLatency, if set, counts slower responses as failures.
	MaxLatency time.Duration
}

type Config struct {
	// Binaries maps each installed core to its executable.
	Binaries map[core.Kind]string
	Priority []core.Kind
	Mode     Mode
	// ManualCore is the core used in Manual mode.
	ManualCore core.Kind

	// WorkDir holds generated configs (they contain credentials) and core data.
	WorkDir string
	// Listen is the SOCKS port the TUN layer forwards to.
	Listen netip.AddrPort
	// ProbeListen is a spare port for trying the primary core before moving back.
	ProbeListen netip.AddrPort

	Health       Health
	StartTimeout time.Duration
	// ReturnToPrimaryAfter is how long to run on a backup core before probing
	// the primary again. Zero disables returning.
	ReturnToPrimaryAfter time.Duration
	LogLevel             string

	// OnEvent receives every event. It is called synchronously from the
	// supervisor's goroutine and must not block.
	OnEvent func(Event)
}

func (c Config) withDefaults() Config {
	if len(c.Priority) == 0 {
		c.Priority = []core.Kind{core.Xray, core.SingBox, core.Mihomo}
	}
	if c.Mode == "" {
		c.Mode = Auto
	}
	if !c.Listen.IsValid() {
		c.Listen = core.DefaultListen
	}
	if !c.ProbeListen.IsValid() {
		c.ProbeListen = netip.AddrPortFrom(c.Listen.Addr(), c.Listen.Port()+1)
	}
	if c.Health.URL == "" {
		c.Health.URL = "http://cp.cloudflare.com/generate_204"
	}
	if c.Health.Interval == 0 {
		c.Health.Interval = 15 * time.Second
	}
	if c.Health.Timeout == 0 {
		c.Health.Timeout = 5 * time.Second
	}
	if c.Health.Failures == 0 {
		c.Health.Failures = 3
	}
	if c.StartTimeout == 0 {
		c.StartTimeout = 10 * time.Second
	}
	if c.LogLevel == "" {
		c.LogLevel = "warn"
	}
	return c
}

type EventKind string

const (
	EventState      EventKind = "state"
	EventSwap       EventKind = "swap"        // Core replaced From
	EventCoreFailed EventKind = "core-failed" // Core was dropped for Reason
	EventHealth     EventKind = "health"
	EventLog        EventKind = "log" // a line of core output
)

type Event struct {
	Time    time.Time
	Kind    EventKind
	State   State
	Core    core.Kind
	From    core.Kind
	Reason  Reason
	Err     error
	Latency time.Duration
	Line    string
	// Probe marks events from trying the primary core on the spare port.
	Probe bool
}

type Status struct {
	State State
	Node  string
	Core  core.Kind
	Chain []core.Kind
	// Failed maps cores dropped during this connection to why.
	Failed map[core.Kind]string
	Since  time.Time
}

type Supervisor struct {
	cfg   Config
	group *proc.Group

	mu     sync.Mutex
	status Status
	cancel context.CancelFunc
	done   chan struct{}
	// active is the core serving Listen; runs counts cores that got there.
	active *process
	runs   int

	// returnReq asks the monitor to move back to the primary core now.
	returnReq chan chan error
}

func New(cfg Config) (*Supervisor, error) {
	cfg = cfg.withDefaults()
	if cfg.WorkDir == "" {
		return nil, errors.New("supervisor: WorkDir is required")
	}
	if len(cfg.Binaries) == 0 {
		return nil, errors.New("supervisor: no core binaries configured")
	}
	// Cores run with their own working directory, against which exec would
	// resolve a relative executable path.
	bins := make(map[core.Kind]string, len(cfg.Binaries))
	for k, p := range cfg.Binaries {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("supervisor: %s: %w", k, err)
		}
		bins[k] = abs
	}
	cfg.Binaries = bins
	g, err := proc.NewGroup()
	if err != nil {
		return nil, fmt.Errorf("supervisor: %w", err)
	}
	return &Supervisor{cfg: cfg, group: g, status: Status{State: Idle}, returnReq: make(chan chan error)}, nil
}

// Connect starts serving n and returns once a core passes its health check,
// or with an error when none can. serverAddr, if not empty, is the already
// resolved address of n's server (see core.Options.ServerAddr). After a
// successful return the supervisor keeps watching and swapping in the
// background until Disconnect.
func (s *Supervisor) Connect(ctx context.Context, n node.Node, serverAddr string) error {
	s.Disconnect()
	chain, err := s.chain(&n)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	ready := make(chan error, 1)
	done := make(chan struct{})
	s.mu.Lock()
	s.cancel, s.done = cancel, done
	s.status = Status{State: Connecting, Node: n.Name, Chain: chain, Failed: map[core.Kind]string{}, Since: time.Now()}
	s.mu.Unlock()

	go func() {
		defer close(done)
		s.run(runCtx, n, serverAddr, chain, ready)
	}()
	select {
	case err := <-ready:
		return err
	case <-ctx.Done():
		s.Disconnect()
		return ctx.Err()
	}
}

// Policy is the part of Config the user can change between connections.
type Policy struct {
	Priority             []core.Kind
	Mode                 Mode
	ManualCore           core.Kind
	Health               Health
	ReturnToPrimaryAfter time.Duration
}

// SetPolicy replaces the swap policy. It stops any running connection, so
// the caller reconnects to apply it.
func (s *Supervisor) SetPolicy(p Policy) {
	s.Disconnect()
	s.mu.Lock() // TestLatency reads the config concurrently
	defer s.mu.Unlock()
	c := s.cfg
	c.Priority, c.Mode, c.ManualCore = slices.Clone(p.Priority), p.Mode, p.ManualCore
	c.Health, c.ReturnToPrimaryAfter = p.Health, p.ReturnToPrimaryAfter
	s.cfg = c.withDefaults()
}

// Disconnect stops the running core. It is a no-op when idle.
func (s *Supervisor) Disconnect() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Chain = slices.Clone(st.Chain)
	st.Failed = maps.Clone(st.Failed)
	return st
}

// Traffic returns the active core's byte counters and which run of a core
// they belong to: counters restart from zero whenever run changes.
func (s *Supervisor) Traffic(ctx context.Context) (t core.Traffic, run int, err error) {
	s.mu.Lock()
	p, run := s.active, s.runs
	s.mu.Unlock()
	if p == nil || !p.stats.IsValid() {
		return core.Traffic{}, run, ErrNotConnected
	}
	t, err = core.ReadTraffic(ctx, p.kind, p.stats, p.secret)
	return t, run, err
}

// ReturnToPrimary moves back to the primary core now instead of waiting for
// ReturnToPrimaryAfter. The primary is tried on the spare port first; if it
// does not work, the current core keeps running and the error says why.
func (s *Supervisor) ReturnToPrimary(ctx context.Context) error {
	s.mu.Lock()
	running := s.cancel != nil
	s.mu.Unlock()
	if !running {
		return ErrNotConnected
	}
	reply := make(chan error, 1)
	select {
	case s.returnReq <- reply:
	case <-time.After(3 * time.Second):
		return errors.New("the core is switching right now; try again in a moment")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Supervisor) chain(n *node.Node) ([]core.Kind, error) { return s.cfg.chain(n) }

// chain returns the installed cores able to run n, in priority order.
func (c Config) chain(n *node.Node) ([]core.Kind, error) {
	var installed []core.Kind
	for _, k := range c.Priority {
		if c.Binaries[k] != "" {
			installed = append(installed, k)
		}
	}
	if c.Mode == Manual {
		k := c.ManualCore
		a, ok := core.ByKind(k)
		if !ok || c.Binaries[k] == "" {
			return nil, fmt.Errorf("%w: %q is not installed", ErrNoCore, k)
		}
		if err := a.Supports(n); err != nil {
			return nil, err
		}
		return []core.Kind{k}, nil
	}
	chain := core.Compatible(n, installed)
	if len(chain) == 0 {
		var why []string
		for _, k := range installed {
			a, _ := core.ByKind(k)
			why = append(why, a.Supports(n).Error())
		}
		return nil, fmt.Errorf("%w (%s)", ErrNoCore, strings.Join(why, "; "))
	}
	return chain, nil
}

func (s *Supervisor) run(ctx context.Context, n node.Node, serverAddr string, chain []core.Kind, ready chan<- error) {
	signal := func(err error) {
		if ready != nil {
			ready <- err
			ready = nil
		}
	}
	failed := map[core.Kind]error{}
	var prev core.Kind
	var prevReason Reason
	for {
		k, ok := firstNotFailed(chain, failed)
		if !ok {
			err := fmt.Errorf("%w: %s", ErrChainExhausted, describe(chain, failed))
			s.setState(Failed, "")
			signal(err)
			return
		}
		if prev == "" {
			s.setState(Connecting, k)
		} else {
			s.setState(Swapping, k)
		}

		p, err := s.launch(ctx, k, n, serverAddr, s.cfg.Listen)
		if err == nil {
			_, err = s.awaitHealthy(ctx, p)
		}
		if ctx.Err() != nil {
			p.stop()
			s.setState(Idle, "")
			signal(ctx.Err())
			return
		}
		if err != nil {
			p.stop()
			s.drop(failed, k, ReasonStartFailed, err)
			prev, prevReason = k, ReasonStartFailed
			continue
		}

		if prev != "" && prev != k {
			s.emit(Event{Kind: EventSwap, Core: k, From: prev, Reason: prevReason})
		}
		s.mu.Lock()
		s.active = p
		s.runs++
		s.mu.Unlock()
		s.setState(Connected, k)
		signal(nil)

		reason, err := s.monitor(ctx, p, n, serverAddr, chain[0])
		s.mu.Lock()
		s.active = nil
		s.mu.Unlock()
		p.stop()
		switch reason {
		case "":
			s.setState(Idle, "")
			return
		case ReasonReturn:
			clear(failed)
			s.mu.Lock()
			clear(s.status.Failed)
			s.mu.Unlock()
		default:
			s.drop(failed, k, reason, err)
		}
		prev, prevReason = k, reason
	}
}

// monitor watches a healthy core and returns why it must be replaced, or ""
// when ctx is cancelled.
func (s *Supervisor) monitor(ctx context.Context, p *process, n node.Node, serverAddr string, primary core.Kind) (Reason, error) {
	tick := time.NewTicker(s.cfg.Health.Interval)
	defer tick.Stop()
	var back <-chan time.Time
	returnAfter := s.cfg.ReturnToPrimaryAfter
	if s.cfg.Mode == Auto && p.kind != primary && returnAfter > 0 {
		back = time.After(returnAfter)
	}
	fails := 0
	for {
		select {
		case <-ctx.Done():
			return "", nil
		case <-p.Exited():
			return ReasonExited, p.ExitError()
		case <-tick.C:
			_, err := s.check(ctx, p)
			if ctx.Err() != nil {
				return "", nil
			}
			if err == nil {
				fails = 0
				continue
			}
			if fails++; fails >= s.cfg.Health.Failures {
				return ReasonHealth, fmt.Errorf("%d health checks in a row failed, last: %w", fails, err)
			}
		case <-back:
			if s.probe(ctx, primary, n, serverAddr) == nil {
				return ReasonReturn, nil
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
				return ReasonReturn, nil
			}
		}
	}
}

// probe starts k on the spare port and reports whether it becomes healthy.
func (s *Supervisor) probe(ctx context.Context, k core.Kind, n node.Node, serverAddr string) error {
	p, err := s.launch(ctx, k, n, serverAddr, s.cfg.ProbeListen)
	if err == nil {
		_, err = s.awaitHealthy(ctx, p)
	}
	p.stop()
	return err
}

// awaitHealthy gives a fresh core up to Health.Failures attempts.
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
		case <-time.After(s.cfg.Health.Interval):
		}
	}
	return 0, fmt.Errorf("health check failed: %w", err)
}

func (s *Supervisor) check(ctx context.Context, p *process) (time.Duration, error) {
	lat, err := checkHealth(ctx, p.listen, s.cfg.Health)
	s.emit(Event{Kind: EventHealth, Core: p.kind, Latency: lat, Err: err, Probe: p.probe})
	return lat, err
}

// launch writes the config for k and starts it listening on listen.
func (s *Supervisor) launch(ctx context.Context, k core.Kind, n node.Node, serverAddr string, listen netip.AddrPort) (*process, error) {
	a, _ := core.ByKind(k)
	probe := listen != s.cfg.Listen
	dir := filepath.Join(s.cfg.WorkDir, string(k))
	if probe {
		dir += "-probe"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	o := core.Options{Listen: listen, LogLevel: s.cfg.LogLevel, ServerAddr: serverAddr}
	if !probe {
		// Traffic counters for the UI. Losing them is not worth failing
		// the core over, so any trouble here just leaves them off.
		if ap, err := freeLoopbackPort(); err == nil {
			o.Stats, o.StatsSecret = ap, randomSecret()
		}
	}
	cfg, err := a.Render(&n, o)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, a.ConfigName())
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return nil, err
	}
	if proc.PortOpen(listen) {
		return nil, fmt.Errorf("listen address %s is already in use", listen)
	}

	pr, err := s.group.Start(proc.Spec{
		Name:   string(k),
		Path:   s.cfg.Binaries[k],
		Args:   a.RunArgs(path, dir),
		Dir:    dir,
		OnLine: func(line string) { s.emit(Event{Kind: EventLog, Core: k, Line: line, Probe: probe}) },
	})
	if err != nil {
		return nil, err
	}
	p := &process{Process: pr, kind: k, listen: listen, probe: probe, stats: o.Stats, secret: o.StatsSecret}
	if err := pr.WaitFor(ctx, s.cfg.StartTimeout, "socks port "+listen.String(), func() bool { return proc.PortOpen(listen) }); err != nil {
		p.stop()
		return nil, err
	}
	return p, nil
}

func (s *Supervisor) drop(failed map[core.Kind]error, k core.Kind, reason Reason, err error) {
	failed[k] = err
	s.mu.Lock()
	s.status.Failed[k] = err.Error()
	s.mu.Unlock()
	s.emit(Event{Kind: EventCoreFailed, Core: k, Reason: reason, Err: err})
}

func (s *Supervisor) setState(st State, k core.Kind) {
	s.mu.Lock()
	s.status.State, s.status.Core = st, k
	s.mu.Unlock()
	s.emit(Event{Kind: EventState, State: st, Core: k})
}

func (s *Supervisor) emit(e Event) {
	if s.cfg.OnEvent != nil {
		e.Time = time.Now()
		s.cfg.OnEvent(e)
	}
}

func randomSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func firstNotFailed(chain []core.Kind, failed map[core.Kind]error) (core.Kind, bool) {
	for _, k := range chain {
		if _, bad := failed[k]; !bad {
			return k, true
		}
	}
	return "", false
}

func describe(chain []core.Kind, failed map[core.Kind]error) string {
	parts := make([]string, 0, len(chain))
	for _, k := range chain {
		parts = append(parts, fmt.Sprintf("%s: %v", k, failed[k]))
	}
	return strings.Join(parts, "; ")
}
