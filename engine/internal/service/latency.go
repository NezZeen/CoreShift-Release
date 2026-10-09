package service

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/doh"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/supervisor"
)

// latencyConcurrency is how many test cores run at once: enough to get
// through a subscription quickly, few enough not to look like a flood.
const latencyConcurrency = 6

// lookupConcurrency bounds the name lookups, and the goroutines, of one
// latency test: a subscription may list thousands of servers.
const lookupConcurrency = 32

// ErrTestRunning means a latency test is already in progress.
var ErrTestRunning = errors.New("a latency test is already running")

// NodeLatency is the last test result of one node.
type NodeLatency struct {
	Subscription string `json:"subscription"`
	Fingerprint  string `json:"fingerprint"`
	Core         string `json:"core,omitempty"`
	// Method is "icmp", "tcp" or "proxy" (through Core).
	Method    string    `json:"method,omitempty"`
	LatencyMS int64     `json:"latency_ms,omitempty"`
	Error     string    `json:"error,omitempty"`
	TestedAt  time.Time `json:"tested_at"`
}

type latencyState struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc     // ends the running test
	results map[string]NodeLatency // by subscription + "/" + fingerprint
}

// stopLatencyTest ends a running latency test, keeping the results so far:
// connecting should not wait for it, least of all on a phone, where test
// cores compete with the connection's for the processor.
func (s *Service) stopLatencyTest() {
	l := &s.latency
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
	}
}

// TestLatency tests the nodes of subscription subID, or of all subscriptions
// when it is empty, and returns the results. Each result is also published
// as a "latency" event as soon as it is known.
func (s *Service) TestLatency(ctx context.Context, subID string) ([]NodeLatency, error) {
	st := s.cfg.Store
	if st == nil {
		return nil, errors.New("no store")
	}
	type ref struct{ sub, fp string }
	var nodes []node.Node
	var refs []ref
	for _, sub := range st.Subscriptions() {
		if subID != "" && sub.ID != subID {
			continue
		}
		fps := sub.Fingerprints()
		for i, n := range sub.Nodes {
			if sub.IsHidden(fps[i]) {
				continue // removed from the list by the user
			}
			nodes = append(nodes, n)
			refs = append(refs, ref{sub.ID, fps[i]})
		}
	}
	if subID != "" && len(refs) == 0 {
		if _, ok := st.Subscription(subID); !ok {
			return nil, errors.New("no such subscription")
		}
	}

	l := &s.latency
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return nil, ErrTestRunning
	}
	l.running = true
	ctx, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	if l.results == nil {
		l.results = map[string]NodeLatency{}
	}
	// Servers that left the subscriptions tested take their results along:
	// a panel that renews its servers would otherwise grow the map for as
	// long as the engine runs, days on a phone.
	current := make(map[string]bool, len(refs))
	for _, r := range refs {
		current[r.sub+"/"+r.fp] = true
	}
	for k := range l.results {
		if (subID == "" || strings.HasPrefix(k, subID+"/")) && !current[k] {
			delete(l.results, k)
		}
	}
	l.mu.Unlock()
	defer func() {
		cancel()
		l.mu.Lock()
		l.running, l.cancel = false, nil
		l.mu.Unlock()
		s.hub.publish(Event{Kind: "latency", Reason: "finished", Subscription: subID})
	}()
	s.hub.publish(Event{Kind: "latency", Reason: "started", Subscription: subID})

	out := make([]NodeLatency, len(nodes))
	report := func(i int, res NodeLatency) {
		res.Subscription, res.Fingerprint, res.TestedAt = refs[i].sub, refs[i].fp, time.Now()
		out[i] = res
		l.mu.Lock()
		l.results[res.Subscription+"/"+res.Fingerprint] = res
		l.mu.Unlock()
		s.hub.publish(Event{Kind: "latency", Subscription: res.Subscription, Fingerprint: res.Fingerprint,
			Core: res.Core, Method: res.Method, LatencyMS: res.LatencyMS, Error: res.Error})
	}
	all := make([]int, len(nodes))
	for i := range all {
		all[i] = i
	}
	names := s.serverNames(ctx)
	if s.Options().LatencyTest == store.LatencyProxy {
		s.proxyLatency(ctx, nodes, all, nil, names, report)
		return out, nil
	}
	// A node the light probes cannot time is measured through its core.
	failed, errs := s.pingLatency(ctx, nodes, names, report)
	s.proxyLatency(ctx, nodes, failed, errs, names, report)
	return out, nil
}

func latencyMS(d time.Duration) int64 { return max(d.Milliseconds(), 1) }

// proxyLatency measures the nodes at indices idx through their cores.
// pingErrs, if set, are why pinging them failed, for the error message.
// names, if set, are the server names this test has looked up already.
func (s *Service) proxyLatency(ctx context.Context, nodes []node.Node, idx []int, pingErrs map[int]error, names *serverNames, report func(int, NodeLatency)) {
	if len(idx) == 0 {
		return
	}
	sub := make([]node.Node, len(idx))
	for j, i := range idx {
		sub[j] = nodes[i]
	}
	addrs := s.resolveServers(ctx, sub, names)
	s.sup.TestLatency(ctx, sub, addrs, latencyConcurrency, func(r supervisor.LatencyResult) {
		i := idx[r.Index]
		res := NodeLatency{Core: string(r.Core), Method: methodProxy}
		switch {
		case r.Err == nil:
			res.LatencyMS = latencyMS(r.Latency)
		case pingErrs[i] != nil:
			res.Error = fmt.Sprintf("ping: %v; proxy: %v", pingErrs[i], r.Err)
		default:
			res.Error = r.Err.Error()
		}
		report(i, res)
	})
}

const (
	methodICMP  = "icmp"
	methodTCP   = "tcp"
	methodProxy = "proxy"

	// Probes at once: more would queue in a phone's radio and read as delay.
	pingConcurrency = 8
	icmpCount       = 3
	icmpTimeout     = time.Second
	// The best of three: the first handshake after a pause may also wake
	// a mobile radio, which takes a hundred milliseconds or more. A first
	// handshake that fails is tried once more (ping.TCP), so a server that
	// does not answer costs about two timeouts, three seconds.
	tcpCount   = 3
	tcpTimeout = 1500 * time.Millisecond

	// dohAfter is how long the system's resolver has on its own before
	// DNS over HTTPS is asked too.
	dohAfter = 2 * time.Second
)

// dohProbe stands for the DNS-over-HTTPS servers (doh.Servers), all IPv4,
// when choosing how to reach them (bindFor).
var dohProbe = netip.MustParseAddr("1.1.1.1")

// overUDP reports whether n's protocol runs over UDP, leaving no TCP port to
// time.
func overUDP(n *node.Node) bool {
	return n.Protocol == node.Hysteria2 || n.Protocol == node.TUIC || n.Protocol == node.WireGuard
}

// pingLatency times each node's server the light way, as Happ's TCP ping
// does: a TCP handshake with its port, all at once, no core started. Over
// UDP there is no port to time, so ICMP instead. Each server and port is
// probed once however many nodes share it. Returned for a test through the
// core are the nodes the probes could not time, whatever the reason: a name
// that did not resolve, a server that did not answer the handshake or
// refused it, ICMP ignored, a tunnel on this computer answering for it.
// The direct path is not the proxy's: a provider may drop handshakes with a
// server, or its DNS fail the server's name, while the proxy protocol gets
// through, as other clients' tests through the proxy show. Only the core
// test says a server is down. names, if set, are the server names this test
// has looked up already.
func (s *Service) pingLatency(ctx context.Context, nodes []node.Node, names *serverNames, report func(int, NodeLatency)) ([]int, map[int]error) {
	if names == nil {
		names = s.serverNames(ctx)
	}
	bind := names.bind

	sem := make(chan struct{}, pingConcurrency)
	limit := func(f func() (time.Duration, error)) func() (time.Duration, error) {
		return func() (time.Duration, error) {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			defer func() { <-sem }()
			return f()
		}
	}
	var mu sync.Mutex
	icmp := map[netip.Addr]func() (time.Duration, error){}
	tcp := map[netip.AddrPort]func() (time.Duration, error){}
	icmpOnce := func(ip netip.Addr) func() (time.Duration, error) {
		mu.Lock()
		defer mu.Unlock()
		if f, ok := icmp[ip]; ok {
			return f
		}
		f := sync.OnceValues(limit(func() (time.Duration, error) {
			return checkPing(ip)(s.cfg.icmpPing(ctx, ip, bindFor(bind, ip)))
		}))
		icmp[ip] = f
		return f
	}
	tcpOnce := func(ap netip.AddrPort) func() (time.Duration, error) {
		mu.Lock()
		defer mu.Unlock()
		if f, ok := tcp[ap]; ok {
			return f
		}
		f := sync.OnceValues(limit(func() (time.Duration, error) {
			return checkPing(ap.Addr())(s.cfg.tcpPing(ctx, ap, bindFor(bind, ap.Addr())))
		}))
		tcp[ap] = f
		return f
	}

	var failed []int
	errs := map[int]error{}
	var wg sync.WaitGroup
	workers := make(chan struct{}, lookupConcurrency)
	for i := range nodes {
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
			// Not started: reported as cancelled, like the rest.
			report(i, NodeLatency{Error: ctx.Err().Error()})
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-workers }()
			n := &nodes[i]
			method := methodTCP
			if overUDP(n) {
				method = methodICMP
			}
			retry := func(err error) {
				if ctx.Err() != nil {
					// Cancelled: no core is started for it.
					report(i, NodeLatency{Method: method, Error: ctx.Err().Error()})
					return
				}
				mu.Lock()
				failed = append(failed, i)
				errs[i] = err
				mu.Unlock()
			}
			ip, err := names.addr(n.Server)
			if err != nil {
				retry(err)
				return
			}
			if overUDP(n) {
				rtt, err := icmpOnce(ip)()
				if err != nil {
					retry(fmt.Errorf("ICMP: %w", err))
					return
				}
				report(i, NodeLatency{Method: methodICMP, LatencyMS: latencyMS(rtt)})
				return
			}
			rtt, err := tcpOnce(netip.AddrPortFrom(ip, n.Port))()
			if err != nil {
				retry(fmt.Errorf("TCP: %w", err))
				return
			}
			report(i, NodeLatency{Method: methodTCP, LatencyMS: latencyMS(rtt)})
		}()
	}
	wg.Wait()
	slices.Sort(failed)
	return failed, errs
}

// bindFor returns b when it can carry probes to ip; the zero Bind otherwise.
// A server on this computer (127.0.0.1, a local proxy chain) is reached by
// loopback only: bound to the physical interface, every probe timed out,
// so its latency failed and switching servers took it for dead.
func bindFor(b ping.Bind, ip netip.Addr) ping.Bind {
	if !b.Source.IsValid() || b.Source.Is4() != ip.Unmap().Is4() || ip.Unmap().IsLoopback() {
		return ping.Bind{}
	}
	return b
}

// errLocalAnswer means a reply was too fast to have come from the server: a
// tunnel on this computer answered it, e.g. another VPN client's.
var errLocalAnswer = errors.New("answered by a tunnel on this computer, not by the server")

func checkPing(ip netip.Addr) func(time.Duration, error) (time.Duration, error) {
	return func(rtt time.Duration, err error) (time.Duration, error) {
		ip = ip.Unmap()
		if err == nil && rtt < time.Millisecond && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
			return 0, errLocalAnswer
		}
		return rtt, err
	}
}

// resolveServers looks up each node's server once per name, as Connect does,
// so test cores get an address rather than resolving it themselves: some
// resolve slowly enough to fail the test, and a name the system's resolver
// fails may still be had over DNS over HTTPS. A name that does not resolve
// is left to the core. names, if set, are the server names this test has
// looked up already.
func (s *Service) resolveServers(ctx context.Context, nodes []node.Node, names *serverNames) []string {
	if names == nil {
		names = s.serverNames(ctx)
	}
	var hostList []string
	seen := map[string]bool{}
	for _, n := range nodes {
		if !seen[n.Server] {
			seen[n.Server] = true
			hostList = append(hostList, n.Server)
		}
	}
	hosts := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	workers := make(chan struct{}, lookupConcurrency)
	for _, host := range hostList {
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break // left to the cores, which are cancelled too
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-workers }()
			ip, err := names.addr(host)
			if err != nil || ip.String() == host {
				return
			}
			mu.Lock()
			hosts[host] = ip.String()
			mu.Unlock()
		}()
	}
	wg.Wait()
	addrs := make([]string, len(nodes))
	for i, n := range nodes {
		addrs[i] = hosts[n.Server]
	}
	return addrs
}

// serverNames looks each server name up once per latency test, for the
// pings and the test cores both.
type serverNames struct {
	s   *Service
	ctx context.Context
	// bind is how pings and DNS over HTTPS leave: through the physical
	// interface, around any tunnel, ours or another VPN client's, which
	// would answer them itself or add its own detour. On Android the app is
	// outside the VPN and the zero Bind takes the default network.
	bind ping.Bind

	mu sync.Mutex
	m  map[string]func() (netip.Addr, error)
}

func (s *Service) serverNames(ctx context.Context) *serverNames {
	bind, err := s.cfg.physical()
	if err != nil {
		bind = ping.Bind{}
	}
	return &serverNames{s: s, ctx: ctx, bind: bind, m: map[string]func() (netip.Addr, error){}}
}

// addr returns host's address, looking it up the first time.
func (n *serverNames) addr(host string) (netip.Addr, error) {
	n.mu.Lock()
	f, ok := n.m[host]
	if !ok {
		f = sync.OnceValues(func() (netip.Addr, error) { return n.s.testAddr(n.ctx, host, n.bind) })
		n.m[host] = f
	}
	n.mu.Unlock()
	return f()
}

// testAddr is serverAddr for a latency test: a name the system's resolver
// fails, or is slow to answer, is also asked over DNS over HTTPS, from
// public resolvers reached by address. A provider's DNS may fail a VPN
// server's name that resolves anywhere else, and the server is then no
// less usable: other clients' tests go through.
func (s *Service) testAddr(ctx context.Context, host string, b ping.Bind) (netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return a, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		ip  netip.Addr
		err error
	}
	sys := make(chan result, 1)
	go func() {
		ip, err := s.serverAddr(ctx, host)
		sys <- result{ip, err}
	}()
	var dohc chan result
	startDoH := func() {
		dohc = make(chan result, 1)
		go func() {
			ips, err := s.cfg.dohLookup(ctx, host, bindFor(b, dohProbe))
			ip, err := preferIPv4(ips, err)
			dohc <- result{ip, err}
		}()
	}
	timer := time.NewTimer(dohAfter)
	defer timer.Stop()
	var sysErr, dohErr error
	for sys != nil || dohc != nil {
		select {
		case r := <-sys:
			if r.err == nil {
				return r.ip, nil
			}
			sysErr, sys = r.err, nil
			if dohc == nil && dohErr == nil {
				startDoH()
			}
		case <-timer.C:
			if dohc == nil && dohErr == nil {
				startDoH()
			}
		case r := <-dohc:
			if r.err == nil {
				return r.ip, nil
			}
			dohErr, dohc = r.err, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("%w; DNS over HTTPS: %v", sysErr, dohErr)
}

// preferIPv4 picks the address to use of a lookup's, as lookupHost does.
func preferIPv4(ips []netip.Addr, err error) (netip.Addr, error) {
	if err != nil {
		return netip.Addr{}, err
	}
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

// dohPool keeps a DNS-over-HTTPS resolver for the latest way out, so the
// lookups of a latency test share its connections.
type dohPool struct {
	mu   sync.Mutex
	bind ping.Bind
	r    *doh.Resolver
}

func (p *dohPool) lookup(ctx context.Context, host string, b ping.Bind) ([]netip.Addr, error) {
	p.mu.Lock()
	if p.r == nil || p.bind != b {
		if p.r != nil {
			p.r.CloseIdleConnections()
		}
		p.bind = b
		p.r = &doh.Resolver{Dial: b.Dialer(doh.DefaultTimeout).DialContext}
	}
	r := p.r
	p.mu.Unlock()
	return r.Lookup(ctx, host)
}

// Latency returns the last test result of a node, if it was tested.
func (s *Service) Latency(subID, fingerprint string) (NodeLatency, bool) {
	l := &s.latency
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.results[subID+"/"+fingerprint]
	return r, ok
}
