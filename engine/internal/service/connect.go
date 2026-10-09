package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"time"

	"coreshift/engine/internal/dnsguard"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/supervisor"
	"coreshift/engine/internal/tunlayer"
)

// ErrDisconnected is what a connection attempt returns when the user
// disconnected while it was under way, or waiting to start.
var ErrDisconnected = errors.New("disconnected while connecting")

// beginOp takes s.op for a long operation, a connection or a switch of
// servers, and returns its context, which Disconnect cancels: connecting
// can take minutes (a core that does not start, a TUN adapter Windows is
// slow to free), and the user's Disconnect, or the service stopping, must
// not wait for it. An operation that was waiting for s.op when Disconnect
// came starts cancelled. end releases s.op.
func (s *Service) beginOp(ctx context.Context) (opCtx context.Context, end func()) {
	s.mu.Lock()
	disc := s.discGen
	s.mu.Unlock()
	s.op.Lock()
	ctx, cancel := context.WithCancelCause(ctx)
	s.mu.Lock()
	if s.discGen != disc {
		cancel(ErrDisconnected)
	}
	s.opSeq++
	id := s.opSeq
	s.opCancel = cancel
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		if s.opSeq == id {
			s.opCancel = nil
		}
		s.mu.Unlock()
		cancel(nil)
		s.op.Unlock()
	}
}

// disconnected reports that ctx ended because of Disconnect.
func disconnected(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), ErrDisconnected)
}

// Connect switches to n, replacing any current connection.
func (s *Service) Connect(ctx context.Context, n node.Node) error {
	ctx, end := s.beginOp(ctx)
	defer end()
	s.fo.reset() // a server chosen by the user starts a new round of switching
	return s.connectOp(ctx, n)
}

// connectOp is Connect with s.op held, by beginOp.
func (s *Service) connectOp(ctx context.Context, n node.Node) error {
	if disconnected(ctx) {
		return ErrDisconnected // the user disconnected before it began
	}
	s.stopLatencyTest()
	s.stopLocked()

	s.mu.Lock()
	s.gen++
	gen, opts := s.gen, s.opts
	s.pending, s.optsPending, s.connOpts = false, false, opts
	s.lastNode, s.hasLast = n, true
	s.mu.Unlock()
	pol := opts.policy()
	// Without the TUN layer the SOCKS port is the proxy the user's programs
	// are set to use, with no credentials to give. Android always has its
	// VPN, and there other apps must not reach the port.
	pol.OpenInbound = !opts.TUN && !s.cfg.AppOutsideVPN
	s.sup.SetPolicy(pol)
	if s.noNetwork() {
		// Nothing would come of it, and the server's name would not even
		// resolve: the connection is made once there is a network.
		s.waitNetwork(n, opts)
		return nil
	}
	s.setStatus(Status{State: Connecting, Node: n.Name, Protocol: string(n.Protocol), TUN: opts.TUN, Since: time.Now()})

	serverIP, err := s.connectLocked(ctx, n, gen, opts)
	s.mu.Lock()
	s.serverIP = serverIP
	s.mu.Unlock()
	s.healthFails.Store(0)
	if err == nil && disconnected(ctx) {
		err = ErrDisconnected // during the last step
	}
	if err != nil {
		s.stopLocked()
		if disconnected(ctx) {
			// Not a failure: what the user asked for.
			s.setStatus(Status{State: Idle, TUN: s.Options().TUN})
			return ErrDisconnected
		}
		if errors.Is(err, errResolve) && s.noNetwork() {
			// The network went away while connecting.
			s.waitNetwork(n, opts)
			return nil
		}
		s.fail(err)
		return err
	}
	st := s.Status()
	st.State = Connected
	st.Since = time.Now()
	s.setStatus(st)
	pingCtx, stop := context.WithCancel(context.Background())
	s.stopPing = stop
	go s.watchTraffic(pingCtx)
	go s.watchNet(pingCtx, gen)
	s.mu.Lock()
	autoDNS := s.autoDNS
	s.mu.Unlock()
	if autoDNS.IsValid() {
		go s.watchNetwork(pingCtx, gen, autoDNS)
	}
	if k, ok := s.cfg.guard.(dnsguard.Keeper); ok && opts.TUN {
		go s.keepDNS(pingCtx, k)
	}
	return nil
}

// keepDNSInterval is how often a guard the system may undo (Linux without
// systemd-resolved, see dnsguard.Keeper) is checked.
const keepDNSInterval = 5 * time.Second

// keepDNS puts the DNS redirect back whenever the system undid it, until
// ctx ends with the connection.
func (s *Service) keepDNS(ctx context.Context, k dnsguard.Keeper) {
	t := time.NewTicker(keepDNSInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		// Under s.op, so it never races a disconnect's Revert.
		if !s.op.TryLock() {
			continue
		}
		if ctx.Err() == nil {
			if err := k.Keep(ctx); err != nil {
				s.hub.publish(Event{Kind: "dns", Error: "не удалось снова направить системный DNS в туннель: " + err.Error()})
			}
		}
		s.op.Unlock()
	}
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
			Line: fmt.Sprintf("сеть сменилась: DNS %s больше нет, теперь система спрашивает %s; переподключаюсь", direct, addrs[0])})
		s.reconnectGen(gen)
		return
	}
}

// reconnectGen reconnects the last node unless connection gen has ended
// meanwhile, e.g. the user disconnected. A failure is reported by Connect.
// A connection held for want of a network counts as going on.
func (s *Service) reconnectGen(gen int) {
	ctx, end := s.beginOp(context.Background())
	defer end()
	s.mu.Lock()
	st := s.status
	n, current := s.lastNode, gen == s.gen && (st.State == Connected || st.State == NoNetwork && !st.Waiting)
	s.mu.Unlock()
	if current {
		_ = s.connectOp(ctx, n)
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
	var resolvers []netip.Addr
	if o.TUN {
		direct = o.DNS.Direct
		// They also stay routed into the TUN (LANResolvers).
		var err error
		resolvers, err = s.systemResolvers(ctx)
		if direct == "" {
			addrs := resolvers
			if err != nil || len(addrs) == 0 {
				direct = "1.1.1.1"
				why := "система не назвала ни одного"
				if err != nil {
					why = err.Error()
				}
				s.hub.publish(Event{Kind: "dns", Error: fmt.Sprintf("системный DNS не найден (%s): прямые адреса узнаю через %s", why, direct)})
			} else {
				direct = addrs[0].String()
				s.mu.Lock()
				s.autoDNS = addrs[0]
				s.mu.Unlock()
			}
		}
	}
	serverIP, err := s.resolveServer(ctx, n.Server)
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

	opts := tunlayer.Options{
		StrictRoute:     true,
		Upstream:        s.cfg.Listen,
		UpstreamUser:    s.socks.User,
		UpstreamPass:    s.socks.Pass,
		BypassProcesses: slices.Sorted(maps.Values(s.cfg.Binaries)),
		AppFilter:       o.AppFilter,
		FilterApps:      o.FilterApps,
		DNS: tunlayer.DNSOptions{
			Remote:          o.DNS.Remote,
			Direct:          direct,
			FakeIP:          o.DNS.FakeIP,
			BlockBrowserDoH: o.DNS.BlockBrowserDoH,
			BlockDoT:        o.DNS.BlockDoT,
		},
		CacheFile: filepath.Join(s.cfg.DataDir, "tun", "cache.db"),
	}
	// The core is up, so a blocked source can be reached through it.
	applyRouting(&opts, o, s.rules.routing(ctx, o, s.proxyURL()))
	if self, err := os.Executable(); err == nil {
		// The daemon resolves proxy servers, e.g. for latency tests.
		opts.DirectDNSProcesses = []string{self}
	}
	switch {
	case s.cfg.ipv6Off():
		// Where the system has IPv6 switched off, an IPv6 address would
		// stop the TUN interface from starting: the tunnel is IPv4-only
		// then, as the system is, and strict_route keeps whatever IPv6 is
		// left out of reach.
	case o.IPv6:
		opts.Address6 = tunlayer.DefaultAddress6
		// Checked before the TUN exists, so its own address cannot count.
		opts.DNS.DirectIPv4Only = !s.cfg.hostIPv6()
	default:
		// IPv6 switched off by the user: the TUN takes it still and
		// refuses it, so it cannot go around the tunnel with the host's
		// real address (see tunlayer.Options.RefuseIPv6).
		opts.RefuseIPv6 = true
	}
	if serverIP.IsValid() {
		opts.BypassAddresses = []netip.Prefix{netip.PrefixFrom(serverIP, serverIP.BitLen())}
	}
	// The local network (a Hyper-V or WSL switch, Docker, a printer) keeps
	// the system's own routes on every platform, rather than leaving by the
	// default interface from the TUN layer (see tunlayer.Options.ExcludeLAN).
	opts.ExcludeLAN = true
	opts.LANResolvers = resolvers
	inst, err := tun.Start(ctx, opts)
	if err != nil && opts.RefuseIPv6 && ctx.Err() == nil && ipv6Refused(err) {
		// The system would not give the interface IPv6 after all (Windows
		// with IPv6 switched off in a way ipv6Off cannot see). IPv4-only,
		// strict_route still keeps IPv6 out of reach.
		s.hub.publish(Event{Kind: "tun", Error: fmt.Sprintf("интерфейс не принял IPv6 (%v): туннель только IPv4", err)})
		opts.RefuseIPv6 = false
		inst, err = tun.Start(ctx, opts)
	}
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
	select {
	case <-inst.Exited():
		// It came up and died at once: not connected, whatever came before.
		// The caller's stopLocked gives the system its DNS back.
		return netip.Addr{}, fmt.Errorf("TUN layer stopped: %w", inst.ExitError())
	default:
	}
	if !s.cfg.AppOutsideVPN {
		s.mu.Lock()
		s.tunDNS = netip.AddrPortFrom(tunlayer.DNSAddress(tunlayer.DefaultAddress), 53)
		s.mu.Unlock()
	}
	return serverIP, nil
}

// applyRouting gives opts the routing of o: the user's lists and rules, the
// presets and their sets.
func applyRouting(opts *tunlayer.Options, o Options, sets geoRouting) {
	suffixes, proxied, pinned, home := routeSuffixes(o)
	opts.DirectApps, opts.DirectIPs, opts.ProxyApps, opts.ProxyIPs = o.DirectApps, o.DirectIPs, o.ProxyApps, o.ProxyIPs
	opts.Selective, opts.Rules = o.Selective, sets.rules
	d := &opts.DNS
	d.DirectSuffixes, d.ProxySuffixes, d.BlockSuffixes = suffixes, proxied, o.BlockDomains
	d.BlockRuleSets, d.PinnedSuffixes, d.PinnedRuleSets, d.ProxyRuleSets = sets.block, pinned, sets.pinned, sets.proxied
	d.HomeSuffixes, d.HomeCheck, d.DirectRuleSets, d.DirectIPRuleSets = home, o.DNS.RussiaAbroad, sets.domain, sets.ip
}

// routeSuffixes returns the names of the user's lists, direct (wherever
// the site is) and through the tunnel, and with the Russian preset
// Google's, pinned to the tunnel unless the user's lists say otherwise,
// and the preset's direct names (see tunlayer.DNSOptions).
func routeSuffixes(o Options) (direct, proxied, pinned, home []string) {
	direct = mergeSuffixes(alwaysDirect, o.DNS.DirectSuffixes)
	proxied = o.ProxyDomains
	if o.DNS.RussiaDirect && !o.Selective {
		direct = mergeSuffixes(direct, russiaAlways)
		pinned = googleSuffixes
		home = russiaSuffixes
	}
	return direct, proxied, pinned, home
}

// Disconnect stops everything and restores the system. Safe when idle. A
// connection or a switch of servers under way, or waiting to start, is
// cancelled rather than waited for.
func (s *Service) Disconnect() {
	s.mu.Lock()
	s.discGen++
	cancel := s.opCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel(ErrDisconnected)
	}
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
	if s.waitCancel != nil {
		s.waitCancel() // a wait for the network ends with its connection
		s.waitCancel = nil
	}
	s.mu.Unlock()
	s.netDown.Store(false)
	if s.stopPing != nil {
		s.stopPing()
		s.stopPing = nil
	}
	s.hub.clearTraffic()
	if err := s.cfg.guard.Revert(context.Background()); err != nil {
		s.hub.publish(Event{Kind: "dns", Error: "не удалось восстановить системный DNS: " + err.Error()})
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
		gen, connected := s.gen, s.status.State == Connected || s.status.State == NoNetwork
		s.mu.Unlock()
		// While connecting, Connect itself reports the failure.
		if connected {
			s.teardown(gen, errors.New("every compatible core failed"))
		}
	}
	switch {
	case e.Kind == supervisor.EventNoBetter:
		// No core gets through: the server or the network is at fault.
		s.mu.Lock()
		gen := s.gen
		s.mu.Unlock()
		s.diagnose(gen)
	case e.Kind == supervisor.EventHealth && e.Err == nil && !e.Probe:
		// The server answers: a later failure starts a fresh round.
		s.healthFails.Store(0)
		s.healthOK.Store(time.Now().UnixNano())
		if s.netDown.Load() {
			// Through the tunnel, so there is a network after all.
			s.netSeen.Store(true)
			s.kickNetwork()
		}
		s.fo.reset()
		s.clearProblem()
	case e.Kind == supervisor.EventHealth && !e.Probe:
		s.healthFails.Add(1)
	}
}

// fail reports a connection that failed. The state event carries the
// error: a second event with it made the journal say it twice.
func (s *Service) fail(err error) {
	s.setStatus(Status{State: Failed, TUN: s.Options().TUN, Error: err.Error(), Since: time.Now()})
}

func (s *Service) setStatus(st Status) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	s.hub.publish(Event{Kind: "state", State: st.State, Core: string(st.Core), Error: st.Error})
}
