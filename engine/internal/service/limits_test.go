package service

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
)

// concurrency counts calls running at once and remembers the most.
type concurrency struct {
	now, most atomic.Int32
}

func (c *concurrency) enter() {
	n := c.now.Add(1)
	for {
		m := c.most.Load()
		if n <= m || c.most.CompareAndSwap(m, n) {
			return
		}
	}
}

func (c *concurrency) leave() { c.now.Add(-1) }

// A subscription with thousands of servers is looked up and pinged a few
// dozen at a time, not all at once.
func TestLatencyLookupsAreBounded(t *testing.T) {
	var c concurrency
	h := newHarness(t, func(cfg *Config) {
		cfg.lookup = func(ctx context.Context, host string, _ netip.AddrPort) (netip.Addr, error) {
			c.enter()
			defer c.leave()
			time.Sleep(2 * time.Millisecond)
			return netip.MustParseAddr("198.51.100.7"), nil
		}
		cfg.tcpPing = func(context.Context, netip.AddrPort, ping.Bind) (time.Duration, error) {
			return 0, errors.New("unreachable")
		}
	})
	nodes := make([]node.Node, 2000)
	for i := range nodes {
		nodes[i] = node.Node{Name: fmt.Sprint(i), Protocol: node.Trojan, Server: fmt.Sprintf("s%d.example.com", i), Port: 443, Password: "pw"}
	}
	addrs := h.svc.resolveServers(context.Background(), nodes, nil)
	if addrs[1999] != "198.51.100.7" {
		t.Errorf("address %q", addrs[1999])
	}
	if m := c.most.Load(); m > lookupConcurrency {
		t.Errorf("%d lookups at once, want at most %d", m, lookupConcurrency)
	}

	c.most.Store(0)
	var mu sync.Mutex
	reported := 0
	failed, _ := h.svc.pingLatency(context.Background(), nodes, nil, func(int, NodeLatency) { mu.Lock(); reported++; mu.Unlock() })
	if m := c.most.Load(); m > lookupConcurrency || m == 0 {
		t.Errorf("%d lookups at once while pinging, want 1..%d", m, lookupConcurrency)
	}
	// None answers the ping: all are left to a test through the core.
	if reported != 0 || len(failed) != len(nodes) {
		t.Errorf("%d results and %d left to cores for %d nodes", reported, len(failed), len(nodes))
	}
}
