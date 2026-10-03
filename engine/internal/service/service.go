// Package service is the engine as the UI sees it: connect a node, disconnect,
// status and events. It assembles the pieces in a fixed order and undoes them
// in reverse, so a failure at any step leaves the system as it was:
//
//	connect:    capture system DNS → resolve server → core (supervisor)
//	            → TUN layer → DNS guard
//	disconnect: DNS guard → TUN layer → core
//
// If the TUN layer dies or every core fails while connected, the service tears
// everything down by itself: DNS pointing at a dead tunnel would leave the
// user without internet.
//
// With a store (see the store package) the service also keeps the user's
// settings and subscriptions, refreshes them in the background and connects
// the selected node; changed settings apply from the next connection.
//
// On desktop the service runs in the privileged daemon behind the HTTP API
// (api.go); on Android the app calls it directly.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/coreupdate"
	"coreshift/engine/internal/dnsguard"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/proc"
	"coreshift/engine/internal/selfupdate"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/supervisor"
	"coreshift/engine/internal/tunlayer"
)

type State string

const (
	Idle          State = "idle"
	Connecting    State = "connecting"
	Connected     State = "connected"
	Disconnecting State = "disconnecting"
	Failed        State = "failed"
)

type Status struct {
	State    State                `json:"state"`
	Node     string               `json:"node,omitempty"`
	Protocol string               `json:"protocol,omitempty"`
	Core     core.Kind            `json:"core,omitempty"`
	Chain    []core.Kind          `json:"chain,omitempty"`
	Failed   map[core.Kind]string `json:"failed,omitempty"`
	TUN      bool                 `json:"tun"`
	Since    time.Time            `json:"since,omitzero"`
	Error    string               `json:"error,omitempty"`
	// Pending is set when settings changed during this connection; they
	// apply after reconnecting.
	Pending bool `json:"settings_pending,omitempty"`
}

type Service struct {
	cfg     Config
	sup     *supervisor.Supervisor
	hub     *hub
	logs    *logGrouper
	rules   *ruleSets
	latency latencyState
	cores   coreState
	upd     appUpdater
	apps    appWatch
	stats   *trafficStats
	fo      failover
	speedMu sync.Mutex // one speed test at a time
	// socks are the credentials of the cores' SOCKS inbound, new each
	// start; only the TUN layer and the service's own clients know them.
	socks core.SOCKSAuth

	// startConn is held by the connection made on the service's own at
	// start (connectAtStart): only one may run at a time.
	startConn sync.Mutex

	op       sync.Mutex // serialises connect, disconnect and teardown
	tun      TUNInstance
	stopPing context.CancelFunc // ends the connection's watchers: traffic, network

	mu sync.Mutex
	// opCancel cancels the operation holding op (beginOp); opSeq tells
	// operations apart, discGen counts Disconnect calls.
	opCancel context.CancelCauseFunc
	opSeq    uint64
	discGen  uint64
	gen      int // incremented per connection; stale teardowns compare it
	status   Status
	opts     Options
	pending  bool      // opts changed since the running connection started
	lastNode node.Node // for Reconnect
	hasLast  bool
	// tunDNS is the TUN layer's resolver while it runs. Server names are
	// looked up there by the daemon itself, which the layer answers with
	// real addresses (tunlayer.Options.DirectDNSProcesses). Through the
	// system resolver the queries would come from the system's DNS service
	// and get fake addresses; straight to the direct resolver they would
	// be blocked by the strict route.
	tunDNS netip.AddrPort
	// autoDNS is the system resolver the TUN layer took for direct names,
	// when the settings leave the choice to the system; watchNetwork
	// follows it.
	autoDNS netip.Addr
}

func New(cfg Config) (*Service, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.Store != nil {
		cfg.Options = OptionsFromSettings(cfg.Store.Settings())
	}
	cfg.Options = cfg.Options.withDefaults()
	if !cfg.Listen.IsValid() {
		cfg.Listen = core.DefaultListen
	}
	if cfg.TUNLayer != nil {
		cfg.tun = cfg.TUNLayer
	}
	if cfg.SystemResolvers != nil {
		cfg.resolvers = cfg.SystemResolvers
	}
	if cfg.PhysicalBind != nil {
		cfg.physical = cfg.PhysicalBind
	}
	if cfg.HostIPv6 != nil {
		cfg.hostIPv6 = cfg.HostIPv6
	}
	if cfg.resolvers == nil {
		cfg.resolvers = dnsguard.SystemResolvers
	}
	if cfg.lookup == nil {
		cfg.lookup = lookupHost
	}
	if cfg.hostIPv6 == nil {
		cfg.hostIPv6 = hostHasIPv6
	}
	if cfg.physical == nil {
		cfg.physical = func() (ping.Bind, error) { return ping.Physical(tunlayer.DefaultInterface) }
	}
	if cfg.icmpPing == nil {
		cfg.icmpPing = func(ctx context.Context, ip netip.Addr, b ping.Bind) (time.Duration, error) {
			return ping.ICMP(ctx, ip, b, icmpCount, icmpTimeout)
		}
	}
	if cfg.tcpPing == nil {
		cfg.tcpPing = func(ctx context.Context, ap netip.AddrPort, b ping.Bind) (time.Duration, error) {
			return ping.TCP(ctx, ap, b, tcpCount, tcpTimeout)
		}
	}
	if cfg.netInterval == 0 {
		cfg.netInterval = networkCheckInterval
	}
	if cfg.checkRelease == nil {
		cfg.checkRelease = func(ctx context.Context, c *http.Client, src selfupdate.Source) (selfupdate.Release, error) {
			return selfupdate.Check(ctx, c, src, selfupdate.ManifestFor(runtime.GOOS), selfupdate.PublicKeys)
		}
	}
	if cfg.downloadRelease == nil {
		cfg.downloadRelease = selfupdate.Download
	}
	if cfg.InstallUpdate != nil {
		cfg.launchInstaller = func(path, _ string) error { return cfg.InstallUpdate(path) }
	}
	if cfg.launchInstaller == nil {
		cfg.launchInstaller = launchInstaller
	}
	if cfg.appSessions == nil {
		cfg.appSessions = appSessions
	}
	if cfg.startApp == nil {
		cfg.startApp = startApp
	}
	if cfg.updateFirstCheck == 0 {
		cfg.updateFirstCheck = cfg.FirstUpdateCheck
	}
	if cfg.updateFirstCheck == 0 {
		cfg.updateFirstCheck = appUpdateFirstCheck
	}
	if cfg.updateTick == 0 {
		cfg.updateTick = appUpdateTick
	}
	// The TUN layer matches core processes by full path.
	bins := make(map[core.Kind]string, len(cfg.Binaries))
	for k, p := range cfg.Binaries {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		bins[k] = abs
	}
	cfg.Binaries = bins
	coreupdate.Cleanup(bins)
	if err := prepareDataDir(cfg.DataDir); err != nil {
		return nil, fmt.Errorf("data directory: %w", err)
	}
	for _, sub := range []string{"work", "tun"} {
		dir := filepath.Join(cfg.DataDir, sub)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		// Generated configs contain credentials.
		if err := restrictDir(dir); err != nil {
			return nil, fmt.Errorf("secure %s: %w", dir, err)
		}
	}

	s := &Service{cfg: cfg, hub: newHub(), opts: cfg.Options, status: Status{State: Idle, TUN: cfg.TUN}, socks: core.NewSOCKSAuth()}
	s.logs = newLogGrouper(logGroupEvery, func(source, line string) {
		s.hub.publish(Event{Kind: "log", Source: source, Line: line})
	})
	s.rules = newRuleSets(filepath.Join(cfg.DataDir, "rules"), s.hub.publish)
	s.stats = openStats(filepath.Join(cfg.DataDir, "traffic.json"))
	s.upd.checkNow = make(chan struct{}, 1)
	s.upd.state = AppUpdate{State: UpdateIdle}
	if !cfg.SelfUpdate {
		s.upd.state = AppUpdate{State: UpdateOff, Reason: "only the installed service of a release build updates itself"}
		if cfg.SelfUpdateOff != "" {
			s.upd.state.Reason = cfg.SelfUpdateOff
		}
		if cfg.AnnounceUpdates {
			s.upd.state = AppUpdate{State: UpdateIdle}
		}
	}
	if cfg.fetchRuleSet != nil {
		s.rules.fetch = cfg.fetchRuleSet
	}
	// The guard is needed even with TUN off, to undo what a crashed run left.
	if cfg.guard == nil {
		g, err := dnsguard.New(filepath.Join(cfg.DataDir, "dnsguard.json"))
		if err != nil {
			return nil, err
		}
		s.cfg.guard = g
	}

	p := cfg.Options.policy()
	sup, err := supervisor.New(supervisor.Config{
		Binaries:             cfg.Binaries,
		Priority:             p.Priority,
		Mode:                 p.Mode,
		ManualCore:           p.ManualCore,
		WorkDir:              filepath.Join(cfg.DataDir, "work"),
		Listen:               cfg.Listen,
		Auth:                 s.socks,
		Health:               p.Health,
		ReturnToPrimaryAfter: p.ReturnToPrimaryAfter,
		Fragment:             p.Fragment,
		OnEvent:              s.onSupervisorEvent,
	})
	if err != nil {
		return nil, err
	}
	s.sup = sup
	if cfg.Store != nil {
		cfg.Store.Watch(s.onStoreChange)
	}
	return s, nil
}

// OpenStore opens the store kept in dataDir, in a directory only the service
// can read: it holds subscription URLs and node credentials.
func OpenStore(dataDir string, opts store.Options) (*store.Store, error) {
	if err := prepareDataDir(dataDir); err != nil {
		return nil, fmt.Errorf("data directory: %w", err)
	}
	dir := filepath.Join(dataDir, "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := restrictDir(dir); err != nil {
		return nil, fmt.Errorf("secure %s: %w", dir, err)
	}
	return store.Open(filepath.Join(dir, "store.json"), opts)
}

// Store returns the store, or nil without one.
func (s *Service) Store() *store.Store { return s.cfg.Store }

// Options returns the options the next connection will use.
func (s *Service) Options() Options {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opts
}

// SetOptions replaces the options. A running connection keeps the old ones
// and is marked pending until it is reconnected.
func (s *Service) SetOptions(o Options) {
	o = o.withDefaults()
	s.mu.Lock()
	s.opts = o
	active := s.status.State == Connecting || s.status.State == Connected
	s.pending = s.pending || active
	if !active {
		s.status.TUN = o.TUN
	}
	s.mu.Unlock()
	s.hub.publish(Event{Kind: "options"})
}

func (s *Service) onStoreChange(c store.Change) {
	if c.What == "settings" {
		s.SetOptions(OptionsFromSettings(s.cfg.Store.Settings()))
	}
	e := Event{Kind: "store", Reason: c.What, Subscription: c.ID}
	if c.Err != nil {
		e.Error = c.Err.Error()
	}
	s.hub.publish(e)
}

// tunLayer returns the TUN layer, creating it on first use.
func (s *Service) tunLayer() (TUNLayer, error) {
	if s.cfg.TUNUnavailable != "" {
		return nil, errors.New(s.cfg.TUNUnavailable)
	}
	if s.cfg.tun != nil {
		return s.cfg.tun, nil
	}
	bin := s.cfg.Binaries[core.SingBox]
	if bin == "" {
		return nil, errors.New("TUN mode needs the sing-box core installed")
	}
	group, err := proc.NewGroup()
	if err != nil {
		return nil, err
	}
	s.cfg.tun = &singBoxTUN{
		group: group, bin: bin, dir: filepath.Join(s.cfg.DataDir, "tun"),
		onLine: func(l string) { s.Log("tun", l) },
	}
	return s.cfg.tun, nil
}

// Recover undoes system changes left by a daemon that did not shut down
// cleanly. Call it once at start.
func (s *Service) Recover(ctx context.Context) error {
	return s.cfg.guard.Recover(ctx)
}

// Compatible returns the installed cores able to run n, in priority order:
// the auto-swap chain the UI shows as badges.
func (s *Service) Compatible(n *node.Node) []core.Kind {
	o := s.Options()
	if o.Mode == supervisor.Manual {
		if s.cfg.Binaries[o.ManualCore] != "" {
			return core.Compatible(n, []core.Kind{o.ManualCore})
		}
		return nil
	}
	var installed []core.Kind
	for _, k := range o.Priority {
		if s.cfg.Binaries[k] != "" {
			installed = append(installed, k)
		}
	}
	return core.Compatible(n, installed)
}

// Subscribe streams events; with replay, recent ones come first.
func (s *Service) Subscribe(replay bool) (<-chan Event, func()) { return s.hub.subscribe(replay) }

func (s *Service) Status() Status {
	s.mu.Lock()
	st := s.status
	st.Pending = s.pending && (st.State == Connected || st.State == Connecting)
	s.mu.Unlock()
	if st.State == Connected || st.State == Connecting {
		sup := s.sup.Status()
		st.Core, st.Chain, st.Failed = sup.Core, sup.Chain, sup.Failed
	}
	return st
}

// Stats returns the traffic of the last days.
func (s *Service) Stats(days int) Stats {
	return s.stats.report(time.Now(), min(max(days, 1), statsKeepDays))
}
