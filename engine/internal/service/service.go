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
	"maps"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

type DNSSettings struct {
	// Remote resolves proxied names through the tunnel.
	Remote string
	// Direct resolves direct names and bootstraps; empty means the system
	// resolver captured just before connecting.
	Direct         string
	FakeIP         bool
	DirectSuffixes []string
	// RussiaDirect sends Russian sites direct: .ru/.su/.рф plus the geosite
	// and geoip rule sets, downloaded on first use.
	RussiaDirect    bool
	BlockBrowserDoH bool
	BlockDoT        bool
	// Strict also disables Windows' smart multi-homed name resolution.
	Strict bool
}

// Options are what the user can change while the service runs. New values
// take effect on the next Connect.
type Options struct {
	Priority             []core.Kind
	Mode                 supervisor.Mode
	ManualCore           core.Kind
	Health               supervisor.Health
	ReturnToPrimaryAfter time.Duration
	// LatencyTest is store.LatencyPing (the default) or store.LatencyProxy.
	LatencyTest string

	// TUN routes all system traffic through the tunnel and guards DNS.
	// Without it only the SOCKS port is served.
	TUN bool
	// IPv6 gives the tunnel an IPv6 address too, so IPv6 traffic goes
	// through it instead of around it.
	IPv6 bool
	DNS  DNSSettings
	// DirectApps are executable names whose traffic bypasses the tunnel.
	DirectApps []string
	// DirectIPs bypass the tunnel too; see tunlayer.Options.
	DirectIPs []netip.Prefix
	// The proxy lists always go through the tunnel; with Selective they are
	// the only traffic that does.
	ProxyDomains []string
	ProxyIPs     []netip.Prefix
	ProxyApps    []string
	Selective    bool
	// BlockDomains are refused.
	BlockDomains []string
}

type Config struct {
	// Options are the initial options; with a Store they come from its
	// settings instead.
	Options

	DataDir  string
	Binaries map[core.Kind]string
	// Listen is the SOCKS port of the active core.
	Listen netip.AddrPort

	// Store, if set, provides settings, subscriptions and the selected node.
	Store *store.Store
	// TUNUnavailable, if set, is why TUN mode cannot work here, e.g. missing
	// privileges; connecting with TUN on then fails with it.
	TUNUnavailable string
	// SelfUpdate lets the service update CoreShift: only the installed
	// service of a release build may run an installer over itself.
	SelfUpdate bool
	// InstallUpdate, if set, hands a verified update to the system's
	// installer, which asks the user (Android): nothing installs by
	// itself, and an update the user declined is offered again.
	InstallUpdate func(path string) error
	// FirstUpdateCheck is how long after the start updates are first
	// looked for; 0 means 2 minutes.
	FirstUpdateCheck time.Duration

	// Hooks for embedding the service in an app (Android); nil means the
	// desktop implementation.
	//
	// TUNLayer runs the TUN + DNS layer.
	TUNLayer TUNLayer
	// SystemResolvers lists the resolvers of the network the device is on.
	SystemResolvers func(ctx context.Context, exclude string) ([]netip.Addr, error)
	// PhysicalBind says how pings of servers leave the device, outside the
	// tunnel.
	PhysicalBind func() (ping.Bind, error)
	// HostIPv6 reports whether the device has IPv6 of its own.
	HostIPv6 func() bool

	// Test seams; nil means the real implementation.
	guard     dnsguard.Guard
	tun       TUNLayer
	resolvers func(ctx context.Context, exclude string) ([]netip.Addr, error)
	// lookup resolves a server name: through server when it is valid,
	// else through the system resolver.
	lookup       func(ctx context.Context, host string, server netip.AddrPort) (netip.Addr, error)
	fetchRuleSet func(ctx context.Context, url string, proxy netip.AddrPort) ([]byte, error)
	hostIPv6     func() bool
	physical     func() (ping.Bind, error)
	icmpPing     func(ctx context.Context, ip netip.Addr, b ping.Bind) (time.Duration, error)
	tcpPing      func(ctx context.Context, ap netip.AddrPort, b ping.Bind) (time.Duration, error)
	pingInterval time.Duration
	netInterval  time.Duration

	checkRelease     func(ctx context.Context, client *http.Client, src selfupdate.Source) (selfupdate.Release, error)
	downloadRelease  func(ctx context.Context, client *http.Client, rel selfupdate.Release, dir string) (string, error)
	launchInstaller  func(path, logPath string) error
	appSessions      func() []uint32
	startApp         func(sessions []uint32) error
	updateFirstCheck time.Duration
	updateTick       time.Duration
}

// DefaultDataDir is where the daemon keeps its state.
func DefaultDataDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "CoreShift")
	}
	return "/var/lib/coreshift"
}

// DefaultDNS is used when the caller has no preference.
var DefaultDNS = DNSSettings{
	Remote: "https://1.1.1.1/dns-query",
	FakeIP: true,
	Strict: true,
}

// alwaysDirect are local names that never make sense through the tunnel.
var alwaysDirect = []string{"lan", "local", "home.arpa"}

// OptionsFromSettings converts the stored settings.
func OptionsFromSettings(set store.Settings) Options {
	c := set.Cores
	o := Options{
		Priority: slices.Clone(c.Priority),
		Mode:     supervisor.Auto,
		Health: supervisor.Health{
			URL:        c.HealthURL,
			Interval:   time.Duration(c.HealthIntervalS) * time.Second,
			Failures:   c.HealthFailures,
			MaxLatency: time.Duration(c.MaxLatencyMS) * time.Millisecond,
		},
		ReturnToPrimaryAfter: time.Duration(c.ReturnAfterMin) * time.Minute,
		LatencyTest:          c.LatencyTest,
		TUN:                  set.TUN,
		IPv6:                 set.IPv6,
		DirectApps:           slices.Clone(set.Routing.DirectApps),
		DirectIPs:            prefixes(set.Routing.DirectIPs),
		ProxyDomains:         slices.Clone(set.Routing.ProxyDomains),
		ProxyIPs:             prefixes(set.Routing.ProxyIPs),
		ProxyApps:            slices.Clone(set.Routing.ProxyApps),
		Selective:            set.Routing.Mode == store.RouteSelected,
		BlockDomains:         slices.Clone(set.Routing.BlockDomains),
		DNS: DNSSettings{
			Remote:          set.DNS.Remote,
			Direct:          set.DNS.Direct,
			FakeIP:          set.DNS.FakeIP,
			DirectSuffixes:  slices.Clone(set.Routing.DirectDomains),
			RussiaDirect:    set.Routing.RussiaDirect,
			BlockBrowserDoH: set.DNS.BlockBrowserDoH,
			BlockDoT:        set.DNS.BlockDoT,
			Strict:          set.DNS.Strict,
		},
	}
	if c.Mode == store.ModeManual {
		o.Mode, o.ManualCore = supervisor.Manual, c.Manual
	}
	return o
}

// prefixes parses the store's address lists, which it has validated.
func prefixes(list []string) []netip.Prefix {
	var out []netip.Prefix
	for _, v := range list {
		if p, err := netip.ParsePrefix(v); err == nil {
			out = append(out, p)
		} else if a, err := netip.ParseAddr(v); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out
}

func (o Options) withDefaults() Options {
	if o.DNS.Remote == "" {
		// No DNS preferences: the defaults, keeping what the caller did set.
		d := DefaultDNS
		d.Direct, d.DirectSuffixes, d.RussiaDirect = o.DNS.Direct, o.DNS.DirectSuffixes, o.DNS.RussiaDirect
		d.BlockBrowserDoH, d.BlockDoT = o.DNS.BlockBrowserDoH, o.DNS.BlockDoT
		o.DNS = d
	}
	if len(o.Priority) == 0 {
		for _, a := range core.Adapters() {
			o.Priority = append(o.Priority, a.Kind())
		}
	}
	return o
}

func (o Options) policy() supervisor.Policy {
	return supervisor.Policy{
		Priority: o.Priority, Mode: o.Mode, ManualCore: o.ManualCore,
		Health: o.Health, ReturnToPrimaryAfter: o.ReturnToPrimaryAfter,
	}
}

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
	rules   *ruleSets
	latency latencyState
	cores   coreState
	upd     appUpdater

	op       sync.Mutex // serialises connect, disconnect and teardown
	tun      TUNInstance
	stopPing context.CancelFunc // ends the connected server's pings

	mu       sync.Mutex
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
	if cfg.pingInterval == 0 {
		cfg.pingInterval = pingInterval
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

	s := &Service{cfg: cfg, hub: newHub(), opts: cfg.Options, status: Status{State: Idle, TUN: cfg.TUN}}
	s.rules = newRuleSets(filepath.Join(cfg.DataDir, "rules"), s.hub.publish)
	s.upd.checkNow = make(chan struct{}, 1)
	s.upd.state = AppUpdate{State: UpdateIdle}
	if !cfg.SelfUpdate {
		s.upd.state = AppUpdate{State: UpdateOff, Reason: "only the installed service of a release build updates itself"}
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
		Health:               p.Health,
		ReturnToPrimaryAfter: p.ReturnToPrimaryAfter,
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

// Log adds a line of output to the event stream, as the TUN layer reports
// it (a separate process on the desktop, part of the app on Android).
func (s *Service) Log(source, line string) {
	if !noiseLine(line) {
		s.hub.publish(Event{Kind: "log", Source: source, Line: line})
	}
}

// noiseLine recognises TUN layer errors that are no fault of the tunnel: a
// name that does not exist (NXDOMAIN, often an ad or tracker host). The
// layer resolves names to match addresses against geoip, and reports every
// such failure as an error, which read like the VPN breaking.
func noiseLine(l string) bool {
	return strings.Contains(l, "NXDOMAIN") && (strings.Contains(l, "dns: lookup failed") || strings.Contains(l, "router: lookup"))
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

// Connect switches to n, replacing any current connection.
func (s *Service) Connect(ctx context.Context, n node.Node) error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.connectOp(ctx, n)
}

// connectOp is Connect with s.op held.
func (s *Service) connectOp(ctx context.Context, n node.Node) error {
	s.stopLocked()

	s.mu.Lock()
	s.gen++
	gen, opts := s.gen, s.opts
	s.pending = false
	s.lastNode, s.hasLast = n, true
	s.mu.Unlock()
	s.sup.SetPolicy(opts.policy())
	s.setStatus(Status{State: Connecting, Node: n.Name, Protocol: string(n.Protocol), TUN: opts.TUN, Since: time.Now()})

	serverIP, err := s.connectLocked(ctx, n, gen, opts)
	if err != nil {
		s.stopLocked()
		s.fail(err)
		return err
	}
	st := s.Status()
	st.State = Connected
	st.Since = time.Now()
	s.setStatus(st)
	pingCtx, stop := context.WithCancel(context.Background())
	s.stopPing = stop
	go s.watchPing(pingCtx, serverIP, n.Port)
	go s.watchTraffic(pingCtx)
	s.mu.Lock()
	autoDNS := s.autoDNS
	s.mu.Unlock()
	if autoDNS.IsValid() {
		go s.watchNetwork(pingCtx, gen, autoDNS)
	}
	return nil
}

// networkCheckInterval is how often a connection that took the system's
// resolver for direct names checks that the resolver is still there.
const networkCheckInterval = 10 * time.Second

// watchNetwork reconnects when the system's resolvers no longer include
// direct, the one the TUN layer took for direct names: the computer moved
// to another network (another Wi-Fi, a phone's hotspot), where that
// resolver is out of reach, so every direct site would stop opening. A
// change has to be seen twice in a row, so a brief flap of an adapter does
// not reconnect, and a computer that is offline waits for a network.
func (s *Service) watchNetwork(ctx context.Context, gen int, direct netip.Addr) {
	t := time.NewTicker(s.cfg.netInterval)
	defer t.Stop()
	seen := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		addrs, err := s.systemResolvers(ctx)
		if err != nil || len(addrs) == 0 || slices.Contains(addrs, direct) {
			seen = 0
			continue
		}
		if seen++; seen < 2 {
			continue
		}
		s.hub.publish(Event{Kind: "dns", Reason: "network-changed",
			Line: fmt.Sprintf("the network changed: resolver %s is gone, the system now uses %s; reconnecting", direct, addrs[0])})
		s.reconnectGen(gen)
		return
	}
}

// reconnectGen reconnects the last node unless connection gen has ended
// meanwhile, e.g. the user disconnected. A failure is reported by Connect.
func (s *Service) reconnectGen(gen int) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	n, current := s.lastNode, gen == s.gen && s.status.State == Connected
	s.mu.Unlock()
	if current {
		_ = s.connectOp(context.Background(), n)
	}
}

// systemResolvers returns the system's resolvers other than the tunnel's:
// those of the network the computer is on.
func (s *Service) systemResolvers(ctx context.Context) ([]netip.Addr, error) {
	addrs, err := s.cfg.resolvers(ctx, tunlayer.DefaultInterface)
	// Another tunnel on the same addresses (sing-box based clients use them
	// by default) would send direct names back into ours.
	own := tunlayer.DefaultAddress.Masked()
	return slices.DeleteFunc(addrs, own.Contains), err
}

// ReturnToPrimary moves back to the first core of the chain now, when a
// backup core is serving. The primary is tried first; if it does not work,
// the backup keeps running.
func (s *Service) ReturnToPrimary(ctx context.Context) error {
	if st := s.Status().State; st != Connected {
		return supervisor.ErrNotConnected
	}
	return s.sup.ReturnToPrimary(ctx)
}

// Reconnect connects the last node again, applying changed options.
func (s *Service) Reconnect(ctx context.Context) error {
	s.mu.Lock()
	n, ok := s.lastNode, s.hasLast
	s.mu.Unlock()
	if !ok {
		return errors.New("nothing to reconnect: no node was connected yet")
	}
	return s.Connect(ctx, n)
}

// ErrNoSelection means the store has no usable selected node.
var ErrNoSelection = errors.New("no node selected")

// ConnectSelected connects the store's selected node.
func (s *Service) ConnectSelected(ctx context.Context) error {
	if s.cfg.Store == nil {
		return errors.New("no store")
	}
	sel, n, ok := s.cfg.Store.Selected()
	if !ok {
		if sel.Subscription != "" {
			return fmt.Errorf("%w: %q is no longer in its subscription", ErrNoSelection, sel.Name)
		}
		return ErrNoSelection
	}
	return s.Connect(ctx, n)
}

// connectLocked returns the address of n's server.
func (s *Service) connectLocked(ctx context.Context, n node.Node, gen int, o Options) (netip.Addr, error) {
	var tun TUNLayer
	if o.TUN {
		var err error
		if tun, err = s.tunLayer(); err != nil {
			return netip.Addr{}, err
		}
	}
	// Both lookups must happen before the DNS guard redirects the system
	// resolver into the tunnel.
	var direct string
	if o.TUN {
		direct = o.DNS.Direct
		if direct == "" {
			addrs, err := s.systemResolvers(ctx)
			if err != nil || len(addrs) == 0 {
				direct = "1.1.1.1"
				s.hub.publish(Event{Kind: "dns", Error: fmt.Sprintf("no system resolver found (%v); using %s for direct names", err, direct)})
			} else {
				direct = addrs[0].String()
				s.mu.Lock()
				s.autoDNS = addrs[0]
				s.mu.Unlock()
			}
		}
	}
	serverIP, err := s.serverAddr(ctx, n.Server)
	if err != nil {
		return netip.Addr{}, err
	}
	serverAddr := ""
	if serverIP.IsValid() && serverIP.String() != n.Server {
		serverAddr = serverIP.String()
	}

	if err := s.sup.Connect(ctx, n, serverAddr); err != nil {
		return netip.Addr{}, err
	}
	if !o.TUN {
		return serverIP, nil
	}

	suffixes := mergeSuffixes(alwaysDirect, o.DNS.DirectSuffixes)
	var domainSets, ipSets, proxySets []tunlayer.RuleSet
	if o.DNS.RussiaDirect && !o.Selective {
		suffixes = mergeSuffixes(suffixes, russiaSuffixes)
		// The core is up, so a blocked source can be reached through it.
		domainSets, ipSets, proxySets = s.rules.get(ctx, russiaSets, s.cfg.Listen)
	}
	opts := tunlayer.Options{
		StrictRoute:     true,
		Upstream:        s.cfg.Listen,
		BypassProcesses: slices.Sorted(maps.Values(s.cfg.Binaries)),
		DirectApps:      o.DirectApps,
		DirectIPs:       o.DirectIPs,
		ProxyApps:       o.ProxyApps,
		ProxyIPs:        o.ProxyIPs,
		Selective:       o.Selective,
		DNS: tunlayer.DNSOptions{
			Remote:           o.DNS.Remote,
			Direct:           direct,
			FakeIP:           o.DNS.FakeIP,
			DirectSuffixes:   suffixes,
			ProxySuffixes:    o.ProxyDomains,
			BlockSuffixes:    o.BlockDomains,
			DirectRuleSets:   domainSets,
			DirectIPRuleSets: ipSets,
			ProxyRuleSets:    proxySets,
			BlockBrowserDoH:  o.DNS.BlockBrowserDoH,
			BlockDoT:         o.DNS.BlockDoT,
		},
		CacheFile: filepath.Join(s.cfg.DataDir, "tun", "cache.db"),
	}
	if self, err := os.Executable(); err == nil {
		// The daemon resolves proxy servers, e.g. for latency tests.
		opts.DirectDNSProcesses = []string{self}
	}
	if o.IPv6 {
		opts.Address6 = tunlayer.DefaultAddress6
		// Checked before the TUN exists, so its own address cannot count.
		opts.DNS.DirectIPv4Only = !s.cfg.hostIPv6()
	}
	if serverIP.IsValid() {
		opts.BypassAddresses = []netip.Prefix{netip.PrefixFrom(serverIP, serverIP.BitLen())}
	}
	inst, err := tun.Start(ctx, opts)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("start TUN layer: %w", err)
	}
	s.tun = inst
	s.hub.publish(Event{Kind: "tun", Reason: "up"})
	go s.watchTUN(inst, gen)

	guardCfg := dnsguard.Config{
		Interface: tunlayer.DefaultInterface,
		Servers:   []netip.Addr{tunlayer.DNSAddress(tunlayer.DefaultAddress)},
		Strict:    o.DNS.Strict,
	}
	if err := s.cfg.guard.Apply(ctx, guardCfg); err != nil {
		return netip.Addr{}, fmt.Errorf("redirect system DNS: %w", err)
	}
	s.hub.publish(Event{Kind: "dns", Reason: "applied"})
	s.mu.Lock()
	s.tunDNS = netip.AddrPortFrom(tunlayer.DNSAddress(tunlayer.DefaultAddress), 53)
	s.mu.Unlock()
	return serverIP, nil
}

// Disconnect stops everything and restores the system. Safe when idle.
func (s *Service) Disconnect() {
	s.op.Lock()
	defer s.op.Unlock()
	if s.Status().State == Idle {
		return
	}
	s.setStatus(Status{State: Disconnecting, TUN: s.Status().TUN})
	s.stopLocked()
	s.setStatus(Status{State: Idle, TUN: s.Options().TUN})
}

// stopLocked undoes a connection in reverse order: DNS first, so the system
// never points at a tunnel that is already gone.
func (s *Service) stopLocked() {
	s.mu.Lock()
	s.gen++ // turns pending teardowns for this connection into no-ops
	s.tunDNS = netip.AddrPort{}
	s.autoDNS = netip.Addr{}
	s.mu.Unlock()
	if s.stopPing != nil {
		s.stopPing()
		s.stopPing = nil
	}
	s.hub.clearTraffic()
	if err := s.cfg.guard.Revert(context.Background()); err != nil {
		s.hub.publish(Event{Kind: "dns", Error: "restore system DNS: " + err.Error()})
	} else if s.tun != nil {
		// Said aloud, so a journal shows the system got its DNS back.
		s.hub.publish(Event{Kind: "dns", Reason: "reverted"})
	}
	if s.tun != nil {
		s.tun.Stop()
		s.tun = nil
		s.hub.publish(Event{Kind: "tun", Reason: "down"})
	}
	s.sup.Disconnect()
}

// teardown is the reaction to a failure while connected. It runs on its own
// goroutine because it is triggered from supervisor callbacks, and stopping
// the supervisor from inside its own callback would deadlock.
func (s *Service) teardown(gen int, cause error) {
	go func() {
		s.op.Lock()
		defer s.op.Unlock()
		s.mu.Lock()
		stale := gen != s.gen
		s.mu.Unlock()
		if stale {
			return
		}
		s.stopLocked()
		s.fail(cause)
	}()
}

func (s *Service) watchTUN(t TUNInstance, gen int) {
	<-t.Exited()
	s.teardown(gen, fmt.Errorf("TUN layer stopped: %w", t.ExitError()))
}

func (s *Service) onSupervisorEvent(e supervisor.Event) {
	s.hub.publish(fromSupervisor(e))
	if e.Kind == supervisor.EventState && e.State == supervisor.Failed {
		s.mu.Lock()
		gen, connected := s.gen, s.status.State == Connected
		s.mu.Unlock()
		// While connecting, Connect itself reports the failure.
		if connected {
			s.teardown(gen, errors.New("every compatible core failed"))
		}
	}
}

func (s *Service) fail(err error) {
	s.setStatus(Status{State: Failed, TUN: s.Options().TUN, Error: err.Error(), Since: time.Now()})
	s.hub.publish(Event{Kind: "error", Error: err.Error()})
}

func (s *Service) setStatus(st Status) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	s.hub.publish(Event{Kind: "state", State: st.State, Core: string(st.Core), Error: st.Error})
}

// serverAddr returns the server as an IP, resolving a hostname with the
// system resolver.
func (s *Service) serverAddr(ctx context.Context, host string) (netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return a, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s.mu.Lock()
	server := s.tunDNS
	s.mu.Unlock()
	a, err := s.cfg.lookup(ctx, host, server)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve server %s: %w", host, err)
	}
	return a, nil
}

func lookupHost(ctx context.Context, host string, server netip.AddrPort) (netip.Addr, error) {
	r := net.DefaultResolver
	if server.IsValid() {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, server.String())
			},
		}
	}
	ips, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, err
	}
	// Prefer IPv4: the TUN layer is IPv4-only by default.
	for _, ip := range ips {
		if ip.Unmap().Is4() {
			return ip.Unmap(), nil
		}
	}
	if len(ips) == 0 {
		return netip.Addr{}, errors.New("no addresses")
	}
	return ips[0], nil
}

func mergeSuffixes(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		for _, v := range l {
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// hostHasIPv6 reports whether the computer has a public IPv6 address of its
// own, i.e. whether it can reach IPv6 hosts without the tunnel.
// relayedIPv6 are addresses of IPv6-over-IPv4 relays, Teredo and 6to4.
// Windows keeps a Teredo address on hosts without IPv6 of their own, and
// such a relay reaches hardly any sites.
var relayedIPv6 = []netip.Prefix{netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2002::/16")}

// nativeIPv6 reports whether ip is a global IPv6 address that means the
// host has IPv6 of its own.
func nativeIPv6(ip netip.Addr) bool {
	if !ip.Is6() || ip.Is4In6() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	return !slices.ContainsFunc(relayedIPv6, func(p netip.Prefix) bool { return p.Contains(ip) })
}

func hostHasIPv6() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Name == tunlayer.DefaultInterface {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			p, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := p.Addr()
			if nativeIPv6(ip) {
				return true
			}
		}
	}
	return false
}
