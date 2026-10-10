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
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
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
	// NoNetwork: the device has no network (netwatch.go). Either a
	// connection waits for one to start (Status.Waiting), or a connection
	// that lost it is held as it is, its tunnel and DNS redirect in place,
	// until it returns.
	NoNetwork State = "no-network"
)

// active reports whether the VPN is on, coming up or waiting for the
// network to do either.
func (st State) active() bool { return st == Connecting || st == Connected || st == NoNetwork }

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
	// Problem is set while connected when no core gets through: whether the
	// server or the network is at fault (a Reach), until a check passes.
	Problem string `json:"problem,omitempty"`
	// Waiting, in state NoNetwork, means nothing is up yet: the connection
	// starts once the network is there. Without it the connection is held.
	Waiting bool `json:"waiting,omitempty"`
	// DirectBlocked is set when this connection found that direct
	// connections do not get through the network while the tunnel works
	// (direct.go): the settings that send traffic direct should go.
	DirectBlocked bool `json:"direct_blocked,omitempty"`
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
	views   views
	stats   *trafficStats
	fo      failover
	retry   afterConnect
	// healthFails counts the active core's failed checks in a row.
	healthFails atomic.Int32
	// verbose keeps the journal's harmless lines (Options.Verbose).
	verbose atomic.Bool
	// direct counts direct connections the network refuses (direct.go).
	direct directWatch
	// bg is set while the device is idle (SetBackground); awake tells the
	// traffic watcher it no longer is.
	bg    atomic.Bool
	awake chan struct{}
	// power is what else the device and the user said about saving power
	// (power.go).
	power   powerState
	speedMu sync.Mutex // one speed test at a time
	// checkupMu: one checkup at a time (checkup.go).
	checkupMu sync.Mutex
	// socks are the credentials of the cores' SOCKS inbound, new each
	// start; only the TUN layer and the service's own clients know them.
	socks core.SOCKSAuth

	// startConn is held by the connection made on the service's own at
	// start (connectAtStart): only one may run at a time.
	startConn sync.Mutex

	op  sync.Mutex // serialises connect, disconnect and teardown
	tun TUNInstance
	// tunSeq counts the TUN layer's starts, so watchTUN tells the running
	// one from one stopped on purpose; tunOpts, tunAt and guardCfg are what
	// the running one was started with, and when, for starting it again on
	// an updated sing-box (coreapply.go). All under op.
	tunSeq   int
	tunOpts  tunlayer.Options
	tunAt    time.Time
	guardCfg dnsguard.Config
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
	connOpts Options // the options the running connection started with
	// pending: a rule set came after the running connection stopped
	// waiting for it (a core update is applied without reconnecting,
	// coreapply.go); optsPending: the options differ from connOpts.
	pending     bool
	optsPending bool
	lastNode    node.Node // for Reconnect
	hasLast     bool
	// serverIP is the connected server's address, for checkReach;
	// diagnosing is set while one runs.
	serverIP   netip.Addr
	diagnosing bool
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
	// waitCancel ends a connection's wait for the network (awaitNetwork).
	waitCancel context.CancelFunc

	// The network, as netwatch.go follows it: netDown is set while a
	// connection is held for want of one; netKick asks the watcher to look
	// now; netSeen tells it that something got through, so there is a
	// network whatever the route table says, and netBlind that the source
	// was caught wrong, until it sees a network again; healthOK is when a
	// check of the active core last passed (Unix nanoseconds).
	netDown  atomic.Bool
	netKick  chan struct{}
	netSeen  atomic.Bool
	netBlind atomic.Bool
	healthOK atomic.Int64
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
	if cfg.ipv6Off == nil {
		cfg.ipv6Off = ipv6Disabled
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
	if cfg.dohLookup == nil {
		cfg.dohLookup = (&dohPool{}).lookup
	}
	if cfg.netInterval == 0 {
		cfg.netInterval = networkCheckInterval
	}
	if cfg.netUp == nil {
		cfg.netUp = cfg.NetworkUp
	}
	if cfg.netName == nil {
		cfg.netName = cfg.NetworkName
	}
	if cfg.netName == nil {
		cfg.netName = defaultNetworkName
	}
	if cfg.netUp == nil {
		cfg.netUp = hasNetwork
	}
	if cfg.netPoll == 0 {
		cfg.netPoll = netPollInterval
	}
	if cfg.netGrace == 0 {
		cfg.netGrace = netBackGrace
	}
	if cfg.netSettle == 0 {
		cfg.netSettle = netSettleTime
	}
	if cfg.netEvidence == 0 {
		cfg.netEvidence = netEvidenceInterval
	}
	if cfg.coreVersion == nil {
		cfg.coreVersion = core.Version
	}
	if cfg.trafficEvery == 0 {
		cfg.trafficEvery = trafficInterval
	}
	if cfg.trafficIdleEvery == 0 {
		cfg.trafficIdleEvery = trafficIdleInterval
	}
	if cfg.trafficHiddenEvery == 0 {
		cfg.trafficHiddenEvery = trafficHiddenInterval
	}
	if cfg.retryDelay == 0 {
		cfg.retryDelay = retryAfterConnectDelay
	}
	if cfg.coreApplyEvery == 0 {
		cfg.coreApplyEvery = coreApplyEvery
	}
	if cfg.coreApplyQuiet == 0 {
		cfg.coreApplyQuiet = coreApplyQuiet
	}
	if cfg.coreApplyRate == 0 {
		cfg.coreApplyRate = coreApplyRate
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

	s := &Service{cfg: cfg, hub: newHub(), opts: cfg.Options, status: Status{State: Idle, TUN: cfg.TUN}, socks: core.NewSOCKSAuth(),
		awake: make(chan struct{}, 1), netKick: make(chan struct{}, 1)}
	s.verbose.Store(cfg.Options.Verbose)
	s.power.off.Store(cfg.Options.NoBatterySaving)
	s.logs = newLogGrouper(logGroupEvery, func(source, line string, at time.Time) {
		s.hub.publish(Event{Kind: "log", Source: source, Line: line, Time: at})
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
	if cfg.ruleSetBaseline != nil {
		s.rules.baseline = cfg.ruleSetBaseline
	}
	s.rules.fetchGeo = cfg.fetchGeo
	// A set connecting stopped waiting for has come: reconnecting applies
	// it, which the app offers as for changed settings.
	s.rules.late = func() {
		s.mu.Lock()
		active := s.status.State.active()
		s.pending = s.pending || active
		s.mu.Unlock()
		if active {
			s.hub.publish(Event{Kind: "options"})
		}
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
		LogLevel:             p.LogLevel,
		Offline:              s.offline,
		OnEvent:              s.onCoreEvent,
	})
	if err != nil {
		return nil, err
	}
	s.sup = sup
	if cfg.Store != nil {
		cfg.Store.Watch(s.onStoreChange)
		cfg.Store.SetVia(s.subscriptionVia)
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
// and is marked pending until it is reconnected, unless they are the same:
// settings outside the options (auto-connect, updates) and a change undone
// leave it as it is.
func (s *Service) SetOptions(o Options) {
	o = o.withDefaults()
	// The journal keeps or drops lines at once; the cores tell more from
	// the next connection.
	s.verbose.Store(o.Verbose)
	s.setBatterySaving(!o.NoBatterySaving)
	s.mu.Lock()
	s.opts = o
	active := s.status.State.active()
	// How much the journal tells is no reason to reconnect: the cores
	// take their log level at the next connection, whenever it comes.
	cmp := o
	cmp.Verbose = s.connOpts.Verbose
	cmp.NoBatterySaving = s.connOpts.NoBatterySaving // applies at once, power.go
	s.optsPending = active && !reflect.DeepEqual(cmp, s.connOpts)
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
	s.noteSubscription(c)
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
		// Line is when the next attempt comes, Error why this one failed.
		onRetry: func(_ int, delay time.Duration, cause error) {
			s.hub.publish(Event{Kind: "tun", Reason: "retry", Line: delay.String(), Error: cause.Error()})
		},
	}
	return s.cfg.tun, nil
}

// Recover undoes system changes left by a daemon that did not shut down
// cleanly. Call it once at start: DNS, and on Linux the TUN layer's ip
// rules, which outlive a sing-box killed with the daemon.
func (s *Service) Recover(ctx context.Context) error {
	removeStaleResolverRules()
	return errors.Join(s.cfg.guard.Recover(ctx), tunlayer.CleanupRoutes(ctx, tunlayer.DefaultInterface))
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
	st.Pending = (s.pending || s.optsPending) && st.State.active()
	s.mu.Unlock()
	// While waiting for the network no core runs yet.
	if st.State.active() && !st.Waiting {
		sup := s.sup.Status()
		st.Core, st.Chain, st.Failed = sup.Core, sup.Chain, sup.Failed
	}
	return st
}

// Stats returns the traffic of the last days.
func (s *Service) Stats(days int) Stats {
	return s.stats.report(time.Now(), min(max(days, 1), statsKeepDays))
}
