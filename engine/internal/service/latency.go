package service

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"time"

	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/supervisor"
)

// latencyConcurrency is how many test cores run at once: enough to get
// through a subscription quickly, few enough not to look like a flood.
const latencyConcurrency = 6

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
	results map[string]NodeLatency // by subscription + "/" + fingerprint
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
		for _, n := range sub.Nodes {
			nodes = append(nodes, n)
			refs = append(refs, ref{sub.ID, n.Fingerprint()})
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
	if l.results == nil {
		l.results = map[string]NodeLatency{}
	}
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.running = false
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
	if s.Options().LatencyTest == store.LatencyProxy {
		s.proxyLatency(ctx, nodes, all, nil, report)
		return out, nil
	}
	// A node the server's network does not answer pings for is still
	// measured, through its core.
	failed, errs := s.pingLatency(ctx, nodes, report)
	s.proxyLatency(ctx, nodes, failed, errs, report)
	return out, nil
}

func latencyMS(d time.Duration) int64 { return max(d.Milliseconds(), 1) }

// proxyLatency measures the nodes at indices idx through their cores.
// pingErrs, if set, are why pinging them failed, for the error message.
func (s *Service) proxyLatency(ctx context.Context, nodes []node.Node, idx []int, pingErrs map[int]error, report func(int, NodeLatency)) {
	if len(idx) == 0 {
		return
	}
	sub := make([]node.Node, len(idx))
	for j, i := range idx {
		sub[j] = nodes[i]
	}
	addrs := s.resolveServers(ctx, sub)
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

	pingConcurrency = 16
	icmpCount       = 3
	icmpTimeout     = time.Second
	tcpCount        = 2
	tcpTimeout      = 3 * time.Second
)

// pingLatency pings each node's server: ICMP, else a TCP handshake with its
// port. Each server and port is probed once however many nodes share it.
// Nodes that could not be pinged are returned with the reason.
func (s *Service) pingLatency(ctx context.Context, nodes []node.Node, report func(int, NodeLatency)) ([]int, map[int]error) {
	// Pings leave through the physical interface, around any tunnel, ours
	// or another VPN client's, which would answer them itself or add its
	// own detour.
	bind, err := s.cfg.physical()
	if err != nil {
		bind = ping.Bind{}
	}

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
	resolved := map[string]func() (netip.Addr, error){}
	icmp := map[netip.Addr]func() (time.Duration, error){}
	tcp := map[netip.AddrPort]func() (time.Duration, error){}
	once := func(host string) func() (netip.Addr, error) {
		mu.Lock()
		defer mu.Unlock()
		if f, ok := resolved[host]; ok {
			return f
		}
		f := sync.OnceValues(func() (netip.Addr, error) { return s.serverAddr(ctx, host) })
		resolved[host] = f
		return f
	}
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
	for i := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := &nodes[i]
			ip, err := once(n.Server)()
			if err != nil {
				report(i, NodeLatency{Method: methodICMP, Error: err.Error()})
				return
			}
			rtt, icmpErr := icmpOnce(ip)()
			if icmpErr == nil {
				report(i, NodeLatency{Method: methodICMP, LatencyMS: latencyMS(rtt)})
				return
			}
			rtt, tcpErr := tcpOnce(netip.AddrPortFrom(ip, n.Port))()
			if tcpErr == nil {
				report(i, NodeLatency{Method: methodTCP, LatencyMS: latencyMS(rtt)})
				return
			}
			mu.Lock()
			failed = append(failed, i)
			errs[i] = fmt.Errorf("ICMP: %v; TCP: %v", icmpErr, tcpErr)
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.Sort(failed)
	return failed, errs
}

// bindFor returns b when it can carry probes to ip; the zero Bind otherwise.
func bindFor(b ping.Bind, ip netip.Addr) ping.Bind {
	if !b.Source.IsValid() || b.Source.Is4() != ip.Unmap().Is4() {
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
// resolve slowly enough to fail the test. A name that does not resolve is
// left to the core.
func (s *Service) resolveServers(ctx context.Context, nodes []node.Node) []string {
	var names []string
	for _, n := range nodes {
		if !slices.Contains(names, n.Server) {
			names = append(names, n.Server)
		}
	}
	hosts := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, host := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ip, err := s.serverAddr(ctx, host)
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

// Latency returns the last test result of a node, if it was tested.
func (s *Service) Latency(subID, fingerprint string) (NodeLatency, bool) {
	l := &s.latency
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.results[subID+"/"+fingerprint]
	return r, ok
}

// pingInterval is how often the connected server is pinged.
const pingInterval = 5 * time.Second

// watchPing pings the connected server until ctx ends and publishes each
// result as a "ping" event: the latency shown for the connection, measured
// like the node list's so the two agree. The proxied health checks measure
// a whole HTTP request, which reads as a much slower connection.
func (s *Service) watchPing(ctx context.Context, ip netip.Addr, port uint16) {
	if !ip.IsValid() {
		return
	}
	t := time.NewTicker(s.cfg.pingInterval)
	defer t.Stop()
	// Once ICMP gets no answer and TCP does, the server blocks ICMP: waiting
	// for its timeout every time would only delay the results.
	tcpOnly := false
	for {
		e := Event{Kind: "ping"}
		bind, err := s.cfg.physical()
		if err != nil {
			bind = ping.Bind{}
		}
		bind = bindFor(bind, ip)
		icmpErr := errors.New("skipped: the server does not answer it")
		if !tcpOnly {
			var rtt time.Duration
			if rtt, icmpErr = checkPing(ip)(s.cfg.icmpPing(ctx, ip, bind)); icmpErr == nil {
				e.Method, e.LatencyMS = methodICMP, latencyMS(rtt)
			}
		}
		if icmpErr != nil {
			rtt, tcpErr := checkPing(ip)(s.cfg.tcpPing(ctx, netip.AddrPortFrom(ip, port), bind))
			if tcpErr == nil {
				e.Method, e.LatencyMS = methodTCP, latencyMS(rtt)
				tcpOnly = true
			} else {
				e.Error = fmt.Sprintf("ICMP: %v; TCP: %v", icmpErr, tcpErr)
			}
		}
		if ctx.Err() != nil {
			return
		}
		s.hub.publish(e)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
