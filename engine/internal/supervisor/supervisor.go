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
	"net/netip"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"coreshift/engine/internal/core"
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
	// healthFloor is the least time between checks of a healthy
	// connection (SetHealthFloor, power.go), in nanoseconds.
	healthFloor atomic.Int64
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
