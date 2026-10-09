// Package supervisor runs proxy cores and swaps between them.
//
// For a node it builds the chain of installed cores able to run it, in
// priority order, starts the first one and watches it. The connection is up
// as soon as the core runs: health checks only decide whether another core
// would do better. When the core fails to start or exits, the next core in
// the chain takes over. When it keeps failing health checks, the other
// cores are tried on a spare port and the first that works takes over;
// when none does, the connection stays as it is rather than going down,
// since the network, not the core, is then likely at fault. After running
// on a backup for a while the primary core is probed on a spare port and
// takes over again once it works.
//
// The SOCKS port the TUN layer (or the user's programs) sends traffic to,
// Config.Listen, is held by the supervisor itself for the whole connection
// (socksgate), in front of whichever core runs. Each core start gets a
// random loopback port of its own and credentials only the engine knows,
// so a swap never frees the port for another program to take, and the TUN
// layer in front never notices more than a short gap: connections made
// meanwhile wait for the next core. A core's random port is used only once
// the system confirms that the core itself listens there (coreListens).
// The traffic is counted there too (Traffic), so the cores run without an
// API of their own.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/proc"
	"coreshift/engine/internal/socksgate"
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
	// ReasonHung: the process runs but its own local port no longer takes
	// connections, so nothing gets through whatever the server does.
	ReasonHung Reason = "hung"
	// ReasonRestart: the core was started again from its executable, which
	// an update replaced (Restart).
	ReasonRestart Reason = "restart"
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
	// ErrNotNeeded means Restart found another core running, or this one
	// started since: the next start of it runs the new executable anyway.
	ErrNotNeeded = errors.New("the core runs its current executable")
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
	// Listen is the SOCKS port the TUN layer forwards to. The supervisor
	// holds it from Connect to Disconnect and relays it to the running
	// core (socksgate).
	Listen netip.AddrPort
	// Auth is required on Listen; zero means New makes random ones
	// (SOCKSAuth). The cores themselves require other credentials, made
	// by New and known to the supervisor alone.
	Auth core.SOCKSAuth
	// OpenInbound leaves Listen without credentials: without the TUN layer
	// it is the proxy the user's programs are set to use, and they have
	// none to give (browsers cannot). The cores keep theirs.
	OpenInbound bool

	Health       Health
	StartTimeout time.Duration
	// ReturnToPrimaryAfter is how long to run on a backup core before probing
	// the primary again. Zero disables returning.
	ReturnToPrimaryAfter time.Duration
	// IdleHealthInterval is how often a healthy connection is checked while
	// the device is idle (SetIdle), when Health.Interval is shorter; zero
	// means a minute.
	IdleHealthInterval time.Duration
	LogLevel           string
	// Fragment splits the TLS ClientHello to the server (core.Options).
	Fragment bool

	// Offline, if set, reports that the device has no network at all (no
	// default route). Failed checks then prove nothing about the core or
	// the server: they are not counted, no other core is tried and a core
	// is not taken for hung, so the connection waits as it is for the
	// network to come back. It is called on every failed check and must be
	// quick.
	Offline func() bool

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
	if c.IdleHealthInterval == 0 {
		c.IdleHealthInterval = time.Minute
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
	// EventNoBetter: the core keeps failing health checks, and no other
	// core passed one either (or none may be tried: a core chosen by hand);
	// the connection stays on it.
	EventNoBetter EventKind = "no-better"
	EventLog      EventKind = "log" // a line of core output
	// EventRestart: Core hung (Reason) and was started again.
	EventRestart EventKind = "core-restart"
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
	// active is the core serving Listen.
	active *process
	// gate holds Listen while connected and relays it to active; gates
	// counts the connections it was opened for.
	gate  *socksgate.Gate
	gates int

	// returnReq asks the monitor to move back to the primary core now;
	// restartReq to start the running core again (Restart).
	returnReq  chan chan error
	restartReq chan restartRequest
	// restarted is answered once the core Restart asked for serves, or
	// could not start; only the run goroutine uses it.
	restarted chan<- error

	// idle is set while the device is idle (SetIdle); awake tells the
	// monitor it no longer is.
	idle  atomic.Bool
	awake chan struct{}
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
	if !cfg.Auth.Set() {
		cfg.Auth = core.NewSOCKSAuth()
	}
	g, err := proc.NewGroup()
	if err != nil {
		return nil, fmt.Errorf("supervisor: %w", err)
	}
	return &Supervisor{cfg: cfg, group: g, status: Status{State: Idle}, returnReq: make(chan chan error),
		restartReq: make(chan restartRequest), awake: make(chan struct{}, 1)}, nil
}

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
	if s.idle.Load() {
		return max(s.cfg.Health.Interval, s.cfg.IdleHealthInterval)
	}
	return s.cfg.Health.Interval
}

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
	gate, err := socksgate.Listen(socksgate.Config{Listen: s.cfg.Listen, Auth: s.cfg.Auth, Open: s.cfg.OpenInbound})
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

// Policy is the part of Config the user can change between connections.
type Policy struct {
	Priority             []core.Kind
	Mode                 Mode
	ManualCore           core.Kind
	Health               Health
	ReturnToPrimaryAfter time.Duration
	Fragment             bool
	OpenInbound          bool // Config.OpenInbound
	// LogLevel is the cores' (Config.LogLevel); "" is the default, warn.
	LogLevel string
}

// SetPolicy replaces the swap policy. It stops any running connection, so
// the caller reconnects to apply it.
func (s *Supervisor) SetPolicy(p Policy) {
	s.Disconnect()
	s.mu.Lock() // TestLatency reads the config concurrently
	defer s.mu.Unlock()
	c := s.cfg
	c.Priority, c.Mode, c.ManualCore = slices.Clone(p.Priority), p.Mode, p.ManualCore
	c.Health, c.ReturnToPrimaryAfter, c.Fragment = p.Health, p.ReturnToPrimaryAfter, p.Fragment
	c.OpenInbound = p.OpenInbound
	c.LogLevel = p.LogLevel
	s.cfg = c.withDefaults()
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

func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Chain = slices.Clone(st.Chain)
	st.Failed = maps.Clone(st.Failed)
	return st
}

// Traffic returns the byte counters of the connection, which the SOCKS
// port counts whatever core runs behind it, and which connection they
// belong to: they restart from zero whenever run changes.
func (s *Supervisor) Traffic() (t core.Traffic, run int, err error) {
	s.mu.Lock()
	gate, run := s.gate, s.gates
	s.mu.Unlock()
	if gate == nil {
		return core.Traffic{}, run, ErrNotConnected
	}
	return gate.Traffic(), run, nil
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

type restartRequest struct {
	kind   core.Kind
	before time.Time
	reply  chan error
}

// Restart starts core k again, when it is the one running and started
// before the given time: an update replaced its executable since, and only
// a new start runs the new one. The new start is tried on a spare port
// first, so a version that does not work here leaves the running core as
// it is (and the error says why); then it takes over as after a swap,
// connections arriving meanwhile waiting for it. Restart returns once it
// serves. ErrNotNeeded means there is nothing to restart.
func (s *Supervisor) Restart(ctx context.Context, k core.Kind, before time.Time) error {
	s.mu.Lock()
	running := s.cancel != nil
	s.mu.Unlock()
	if !running {
		return ErrNotConnected
	}
	req := restartRequest{kind: k, before: before, reply: make(chan error, 1)}
	select {
	case s.restartReq <- req:
	case <-time.After(3 * time.Second):
		return errors.New("the core is switching right now; try again in a moment")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.reply:
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
	// restartDone answers a Restart under way, if any.
	restartDone := func(err error) {
		if s.restarted != nil {
			s.restarted <- err
			s.restarted = nil
		}
	}
	defer func() { restartDone(ErrNotConnected) }()
	failed := map[core.Kind]error{}
	restarted := map[core.Kind]time.Time{} // the last restart of a hung core
	var prev core.Kind
	var prevReason Reason
	// next is a core already seen working on the spare port.
	var next core.Kind
	for {
		k, ok := next, next != ""
		next = ""
		if !ok {
			k, ok = firstNotFailed(chain, failed)
		}
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

		p, err := s.launch(ctx, k, n, serverAddr, false)
		if ctx.Err() != nil {
			p.stop()
			s.setState(Idle, "")
			signal(ctx.Err())
			return
		}
		if err != nil {
			p.stop()
			s.drop(failed, k, ReasonStartFailed, err)
			restartDone(err)
			prev, prevReason = k, ReasonStartFailed
			continue
		}

		if prev != "" && prev != k {
			s.emit(Event{Kind: EventSwap, Core: k, From: prev, Reason: prevReason})
		}
		s.serve(p)
		s.setState(Connected, k)
		signal(nil)
		restartDone(nil)

		reason, alt, err := s.monitor(ctx, p, n, serverAddr, chain, failed)
		// Connections to it are closed before it stops: their clients learn
		// at once, and new ones wait for the next core.
		s.serve(nil)
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
		case ReasonHealth:
			s.drop(failed, k, reason, err)
			next = alt
		case ReasonRestart:
			next = k
		case ReasonHung:
			// A hung process is restarted once; hanging again soon after
			// means the core itself is the trouble, and it is dropped.
			if time.Since(restarted[k]) > hungRestartWindow {
				restarted[k] = time.Now()
				s.emit(Event{Kind: EventRestart, Core: k, Reason: reason, Err: err})
				next = k
			} else {
				s.drop(failed, k, reason, err)
			}
		default:
			s.drop(failed, k, reason, err)
		}
		prev, prevReason = k, reason
	}
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
		case <-back:
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

// offline reports what Config.Offline says, false without it.
func (s *Supervisor) offline() bool { return s.cfg.Offline != nil && s.cfg.Offline() }

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
