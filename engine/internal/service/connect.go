package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"coreshift/engine/internal/dnsguard"
	"coreshift/engine/internal/msg"
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
	return s.connectNode(ctx, n, true)
}

// connectNode is connectOp; mayKeep false rebuilds a running TUN layer
// even when only the server changes, as after a change of network, whose
// resolvers the layer has to look up again.
func (s *Service) connectNode(ctx context.Context, n node.Node, mayKeep bool) error {
	if disconnected(ctx) {
		return ErrDisconnected // the user disconnected before it began
	}
	s.stopLatencyTest()
	s.stopNodeSpeed()
	// A switch of servers keeps the TUN layer — its interface, the DNS
	// redirect and the fake-IP records applications hold — when nothing but
	// the server changes: rebuilding the layer takes seconds, and a request
	// falling between the old layer and the new fails (the journal's
	// "missing fakeip record" is one). The route to the server is the
	// engine's own (serverroute.go), so the layer's config does not differ.
	s.mu.Lock()
	keepTUN := mayKeep && s.opts.TUN && s.tun != nil && s.status.State == Connected && !s.pending && !s.optsPending && s.hasTunLookups
	s.mu.Unlock()
	if keepTUN && s.networkGone(ctx) {
		// No network: another server would not answer either; wait it out.
		keepTUN = false
	}
	if !keepTUN {
		s.stopLocked()
	}
	s.mu.Lock()
	if !keepTUN {
		s.gen++
	}
	gen, opts := s.gen, s.opts
	s.pending, s.optsPending, s.connOpts = false, false, opts
	s.lastNode, s.hasLast = n, true
	s.mu.Unlock()
	pol := opts.policy()
	// Without the TUN layer the SOCKS port is the proxy the user's programs
	// are set to use, with no credentials to give. Android always has its
	// VPN, and there other apps must not reach the port.
	pol.OpenInbound = !opts.TUN && !s.cfg.AppOutsideVPN
	pol.Inbound = s.inbound(opts)
	if !keepTUN {
		// The policy is the running connection's already: SetPolicy would
		// stop the cores, which connectLocked needs for its lookups.
		s.sup.SetPolicy(pol)
	}
	s.setStatus(Status{State: Connecting, Node: n.Name, Protocol: string(n.Protocol), TUN: opts.TUN, Since: time.Now()})
	if !keepTUN && s.networkGone(ctx) {
		// Nothing would come of it, and the server's name would not even
		// resolve: the connection is made once there is a network.
		s.waitNetwork(n, opts)
		return nil
	}

	res, err := s.connectLocked(ctx, n, gen, opts, pol, keepTUN)
	s.mu.Lock()
	s.serverIP = res.serverIP
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
	if res.keptTUN {
		// The connection's watchers, made with the layer, serve on.
		return nil
	}
	pingCtx, stop := context.WithCancel(context.Background())
	s.stopPing = stop
	go s.watchTraffic(pingCtx)
	go s.watchNet(pingCtx, res.gen)
	s.mu.Lock()
	autoDNS := s.autoDNS
	s.mu.Unlock()
	if autoDNS.IsValid() {
		go s.watchNetwork(pingCtx, res.gen, autoDNS)
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
				s.hub.publish(Event{Kind: "dns"}.withError(msg.New("dns.keep_failed", "err", err)))
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
// not reconnect, and a computer that is offline waits for a network. With
// a phone's screen off it looks seldom (resolverEvery, power.go).
func (s *Service) watchNetwork(ctx context.Context, gen int, direct netip.Addr) {
	seen := 0
	t := time.NewTimer(s.resolverEvery(seen))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		addrs, err := s.systemResolvers(ctx)
		if err != nil || len(addrs) == 0 || slices.Contains(addrs, direct) {
			seen = 0
			t.Reset(s.resolverEvery(seen))
			continue
		}
		if seen++; seen < 2 {
			// Confirmed at the usual pace, screen off or not (power.go).
			t.Reset(s.resolverEvery(seen))
			continue
		}
		s.hub.publish(Event{Kind: "dns", Reason: "network-changed"}.withLine(
			msg.New("dns.network_changed", "old", direct.String(), "new", addrs[0].String())))
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
		// The network changed: its resolvers are looked up again, so the
		// TUN layer is built anew.
		_ = s.connectNode(ctx, n, false)
	}
}

// systemResolvers returns the system's resolvers other than the tunnel's:
// those of the network the computer is on, that can be dialled directly.
// A router announcing an IPv6 link-local resolver (fe80::…, Android on a
// home Wi-Fi) is left out: without its interface every direct lookup sent
// to it failed with "invalid argument". Apps keep asking the tunnel's
// resolver (the DNS guard, the VpnService), whatever the router announced.
func (s *Service) systemResolvers(ctx context.Context) ([]netip.Addr, error) {
	addrs, err := s.cfg.resolvers(ctx, tunlayer.DefaultInterface)
	// Another tunnel on the same addresses (sing-box based clients use them
	// by default) would send direct names back into ours.
	own := tunlayer.DefaultAddress.Masked()
	return dnsguard.FilterUsable(slices.DeleteFunc(addrs, own.Contains)), err
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

// connectResult is how a connection was made.
type connectResult struct {
	serverIP netip.Addr // the connected server's address
	// gen is the connection's generation: a kept TUN layer turns out to be
	// unusable now and then, and the rebuild starts a new generation.
	gen int
	// keptTUN: the previous connection's TUN layer serves on (interface,
	// DNS redirect, fake-IP records untouched), and its watchers with it.
	keptTUN bool
}

// connectLocked makes the connection: the core (supervisor), then with TUN
// the TUN layer and the DNS guard. keep allows keeping a running TUN layer
// when only the server changes (tunReusable).
func (s *Service) connectLocked(ctx context.Context, n node.Node, gen int, o Options, pol supervisor.Policy, keep bool) (connectResult, error) {
	var tun TUNLayer
	if o.TUN {
		var err error
		if tun, err = s.tunLayer(); err != nil {
			return connectResult{}, err
		}
	}
	// Both lookups must happen before the DNS guard redirects the system
	// resolver into the tunnel; a kept layer has its own (tunLookups).
	var direct string
	var resolvers []netip.Addr
	var domains []string
	s.mu.Lock()
	saved, hasSaved := s.tunLookups, s.hasTunLookups
	s.mu.Unlock()
	if o.TUN && keep && hasSaved {
		direct, resolvers, domains = saved.direct, saved.resolvers, saved.domains
	} else if o.TUN {
		direct = o.DNS.Direct
		// The network's own names (tunlayer.DNSOptions.LocalSuffixes);
		// none known is no reason not to connect.
		domains, _ = s.cfg.domains(ctx, tunlayer.DefaultInterface)
		// They also stay routed into the TUN (LANResolvers).
		var err error
		resolvers, err = s.systemResolvers(ctx)
		if direct == "" {
			addrs := resolvers
			if err != nil || len(addrs) == 0 {
				direct = "1.1.1.1"
				why := msg.New("dns.system_none")
				if err != nil {
					why = msg.Raw(err.Error())
				}
				s.hub.publish(Event{Kind: "dns"}.withError(msg.New("dns.system_missing", "why", why, "direct", direct)))
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
		return connectResult{}, err
	}
	serverAddr := ""
	if serverIP.IsValid() && serverIP.String() != n.Server {
		serverAddr = serverIP.String()
	}

	if o.TUN && keep {
		opts := s.tunOptions(ctx, o, direct, resolvers, domains)
		if s.tunReusable(opts, o) {
			// The route first: the core dials the new server as soon as it
			// starts, and its traffic must not enter the tunnel it serves.
			if err := s.addServerRoute(serverIP); err == nil {
				if err := s.sup.Connect(ctx, n, serverAddr); err != nil {
					return connectResult{serverIP: serverIP, gen: gen}, err
				}
				return connectResult{serverIP: serverIP, gen: gen, keptTUN: true}, nil
			}
			// Routes are not managed here (an IPv6 server, no rights): the
			// layer's config has to carry the server, so it is rebuilt.
		}
		// Not reusable: down with the kept layer, then the classic way.
		s.stopLocked()
		s.mu.Lock()
		s.gen++
		gen = s.gen
		s.mu.Unlock()
		s.sup.SetPolicy(pol)
	}

	if err := s.sup.Connect(ctx, n, serverAddr); err != nil {
		return connectResult{serverIP: serverIP, gen: gen}, err
	}
	if !o.TUN {
		s.proxyUp(o)
		return connectResult{serverIP: serverIP, gen: gen}, nil
	}

	opts := s.tunOptions(ctx, o, direct, resolvers, domains)
	if len(domains) > 0 && opts.DNS.LocalResolver.IsValid() {
		// How many, not which: a journal sent to support would name the
		// company the user works at.
		s.hub.publish(Event{Kind: "dns", Reason: "local-names"}.withLine(
			msg.New("dns.local_names", "count", len(domains), "dns", opts.DNS.LocalResolver.String())))
	}
	if serverIP.IsValid() {
		if err := s.addServerRoute(serverIP); err != nil {
			// No route management here: the layer excludes the server
			// itself, and a switch of servers restarts it.
			opts.BypassAddresses = []netip.Prefix{netip.PrefixFrom(serverIP, serverIP.BitLen())}
		}
	}
	started := time.Now()
	inst, err := tun.Start(ctx, opts)
	if err != nil && opts.RefuseIPv6 && ctx.Err() == nil && ipv6Refused(err) {
		// The system would not give the interface IPv6 after all (Windows
		// with IPv6 switched off in a way ipv6Off cannot see). IPv4-only,
		// strict_route still keeps IPv6 out of reach.
		s.hub.publish(Event{Kind: "tun"}.withError(msg.New("tun.ipv6_refused", "err", msg.Raw(err.Error()))))
		opts.RefuseIPv6 = false
		inst, err = tun.Start(ctx, opts)
	}
	if err != nil {
		return connectResult{serverIP: serverIP, gen: gen}, fmt.Errorf("start TUN layer: %w", err)
	}
	s.tunUp(inst, gen, opts, started)
	s.hub.publish(Event{Kind: "tun", Reason: "up"})
	if len(opts.DNS.DirectFallback) > 0 {
		s.dnsDirect.arm(gen, opts.DNS.Direct)
	}

	s.guardCfg = dnsguard.Config{
		Interface: tunlayer.DefaultInterface,
		Servers:   []netip.Addr{tunlayer.DNSAddress(tunlayer.DefaultAddress)},
		Strict:    o.DNS.Strict,
	}
	if err := s.cfg.guard.Apply(ctx, s.guardCfg); err != nil {
		return connectResult{serverIP: serverIP, gen: gen}, fmt.Errorf("redirect system DNS: %w", err)
	}
	s.hub.publish(Event{Kind: "dns", Reason: "applied"})
	select {
	case <-inst.Exited():
		// It came up and died at once: not connected, whatever came before.
		// The caller's stopLocked gives the system its DNS back.
		return connectResult{serverIP: serverIP, gen: gen}, fmt.Errorf("TUN layer stopped: %w", inst.ExitError())
	default:
	}
	s.mu.Lock()
	if !s.cfg.AppOutsideVPN {
		s.tunDNS = netip.AddrPortFrom(tunlayer.DNSAddress(tunlayer.DefaultAddress), 53)
	}
	s.tunLookups, s.hasTunLookups = tunLookups{direct: direct, resolvers: resolvers, domains: domains}, true
	s.mu.Unlock()
	return connectResult{serverIP: serverIP, gen: gen}, nil
}

// tunLookups are the system lookups a TUN layer is built from: the resolver
// direct names go to, the system's resolvers, the network's own names.
type tunLookups struct {
	direct    string
	resolvers []netip.Addr
	domains   []string
}

// tunOptions builds the TUN layer's options for a connection with o: direct
// is the resolver direct names go to, resolvers the system's (they stay
// routed into the TUN), domains the network's own names. The server itself
// is not here: it is routed around the tunnel by addServerRoute, or, where
// routes are not managed, carried in BypassAddresses by the caller.
func (s *Service) tunOptions(ctx context.Context, o Options, direct string, resolvers []netip.Addr, domains []string) tunlayer.Options {
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
			DirectFallback:  directFallback(o.DNS.Direct, resolvers),
			LocalSuffixes:   domains,
			LocalResolver:   firstOr(resolvers),
			FakeIP:          o.DNS.FakeIP,
			BlockBrowserDoH: o.DNS.BlockBrowserDoH,
			BlockDoT:        o.DNS.BlockDoT,
		},
		CacheFile: filepath.Join(s.cfg.DataDir, "tun", "cache.db"),
		LogLevel:  o.logLevel(),
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
	// The local network (a Hyper-V or WSL switch, Docker, a printer) keeps
	// the system's own routes on every platform, rather than leaving by the
	// default interface from the TUN layer (see tunlayer.Options.ExcludeLAN).
	opts.ExcludeLAN = true
	opts.LANResolvers = resolvers
	return opts
}

// tunReusable reports whether the running TUN layer can serve a connection
// that would be made with opts now: its options are the same, and the DNS
// guard's too. A layer that carries its server in BypassAddresses (routes
// are not managed here) is never reusable: a new server means new options.
func (s *Service) tunReusable(opts tunlayer.Options, o Options) bool {
	s.mu.Lock()
	tun, tunOpts, strict := s.tun, s.tunOpts, s.guardCfg.Strict
	s.mu.Unlock()
	if tun == nil || len(tunOpts.BypassAddresses) > 0 {
		return false
	}
	return strict == o.DNS.Strict && reflect.DeepEqual(tunOpts, opts)
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
	s.hasTunLookups = false
	s.dnsDirect.disarm()
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
		s.hub.publish(Event{Kind: "dns"}.withError(msg.New("dns.revert_failed", "err", msg.Raw(err.Error()))))
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
	s.dropServerRoute()
	s.proxyDown()
	// The repeats counted so far belong to this connection: told now,
	// before "отключено", not seconds after it.
	s.logs.flushAll()
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

// tunUp makes inst, started at started with o, connection gen's TUN layer.
// Called with s.op held.
func (s *Service) tunUp(inst TUNInstance, gen int, o tunlayer.Options, started time.Time) {
	s.tunSeq++
	s.tun, s.tunOpts, s.tunAt = inst, o, started
	go s.watchTUN(inst, gen, s.tunSeq)
}

// watchTUN tears connection gen down when its TUN layer, start seq, dies;
// not when it was stopped to start again (applyTUN), which counts a start.
func (s *Service) watchTUN(t TUNInstance, gen, seq int) {
	<-t.Exited()
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	stale := gen != s.gen
	s.mu.Unlock()
	if stale || seq != s.tunSeq {
		return
	}
	s.stopLocked()
	s.fail(fmt.Errorf("TUN layer stopped: %w", t.ExitError()))
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
		s.directChecked(true)
	case e.Kind == supervisor.EventHealth && !e.Probe:
		s.healthFails.Add(1)
		s.directChecked(false)
	}
}

// fail reports a connection that failed. The state event carries the
// error: a second event with it made the journal say it twice.
func (s *Service) fail(err error) {
	st := Status{State: Failed, TUN: s.Options().TUN, Error: err.Error(), Since: time.Now()}
	if m, ok := msg.Of(err); ok {
		st.ErrorCode, st.ErrorArgs = m.Code, m.Args
	}
	s.setStatus(st)
}

func (s *Service) setStatus(st Status) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	s.hub.publish(stateEvent(st))
	if st.State == Connected {
		s.retryAfterConnect()
	}
}
