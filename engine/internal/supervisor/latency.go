package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/proc"
)

// LatencyResult is the outcome of testing one node.
type LatencyResult struct {
	// Index is the node's position in the tested slice.
	Index   int
	Core    core.Kind
	Latency time.Duration
	Err     error
}

// latencyTimeout bounds the request through a test core; starting the core
// has its own limit (StartTimeout).
const latencyTimeout = 5 * time.Second

// TestLatency measures each node the way the health check does: it starts
// the node's first core on a free port, fetches the health URL through it
// and stops it. When that core fails, the node's next cores are tried in
// turn, as a real connection would swap to them, and the result names the
// core that worked. The result is the real delay through the proxy, and a
// node no core can use shows up as an error. Up to concurrency nodes are tested
// at once, independently of any running connection. serverAddrs, if not
// nil, holds each node's resolved server address (see core.Options.
// ServerAddr); without one the core resolves the name itself, which some
// cores do slowly. onResult is called for each node as it finishes, from
// several goroutines.
func (s *Supervisor) TestLatency(ctx context.Context, nodes []node.Node, serverAddrs []string, concurrency int, onResult func(LatencyResult)) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				onResult(LatencyResult{Index: i, Err: ctx.Err()})
				return
			}
			defer func() { <-sem }()
			var addr string
			if i < len(serverAddrs) {
				addr = serverAddrs[i]
			}
			r := s.testNode(ctx, cfg, &nodes[i], addr)
			r.Index = i
			onResult(r)
		}()
	}
	wg.Wait()
}

func (s *Supervisor) testNode(ctx context.Context, cfg Config, n *node.Node, serverAddr string) LatencyResult {
	chain, err := cfg.chain(n)
	if err != nil {
		return LatencyResult{Err: err}
	}
	var errs []error
	for _, k := range chain {
		lat, err := s.testCore(ctx, cfg, k, n, serverAddr)
		if err == nil {
			return LatencyResult{Core: k, Latency: lat}
		}
		errs = append(errs, fmt.Errorf("%s: %w", k, err))
		if ctx.Err() != nil {
			break
		}
	}
	return LatencyResult{Core: chain[0], Err: errors.Join(errs...)}
}

// testCore measures n through one core. A failure carries the core's last
// log line, which usually says why.
func (s *Supervisor) testCore(ctx context.Context, cfg Config, k core.Kind, n *node.Node, serverAddr string) (time.Duration, error) {
	a, _ := core.ByKind(k)
	listen, err := freeLoopbackPort()
	if err != nil {
		return 0, err
	}
	base := filepath.Join(cfg.WorkDir, "latency")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return 0, err
	}
	dir, err := os.MkdirTemp(base, string(k)+"-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	conf, err := a.Render(n, core.Options{Listen: listen, LogLevel: cfg.LogLevel, ServerAddr: serverAddr})
	if err != nil {
		return 0, err
	}
	path := filepath.Join(dir, a.ConfigName())
	if err := os.WriteFile(path, conf, 0o600); err != nil {
		return 0, err
	}
	var last lastLine
	p, err := s.group.Start(proc.Spec{Name: string(k), Path: cfg.Binaries[k], Args: a.RunArgs(path, dir), Dir: dir, OnLine: last.set})
	if err != nil {
		return 0, err
	}
	err = p.WaitFor(ctx, cfg.StartTimeout, "socks port", func() bool { return proc.PortOpen(listen) })
	var lat time.Duration
	if err == nil {
		lat, err = checkHealth(ctx, listen, Health{URL: cfg.Health.URL, Timeout: latencyTimeout})
	}
	p.Stop()
	if err != nil {
		if l := last.get(); l != "" {
			err = fmt.Errorf("%w; core log: %s", err, l)
		}
		return 0, err
	}
	return max(lat, time.Microsecond), nil // the clock may not tick on loopback
}

// lastLine keeps the last line a test core logged. Cores run at warning
// level, so that line is normally the error behind a failed request.
type lastLine struct {
	mu   sync.Mutex
	line string
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func (l *lastLine) set(line string) {
	line = strings.TrimSpace(ansiEscape.ReplaceAllString(line, ""))
	if line == "" {
		return
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	l.mu.Lock()
	l.line = line
	l.mu.Unlock()
}

func (l *lastLine) get() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.line
}

// freeLoopbackPort asks the system for a loopback port nobody uses right now.
func freeLoopbackPort() (netip.AddrPort, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("find a free port: %w", err)
	}
	defer ln.Close()
	return netip.ParseAddrPort(ln.Addr().String())
}
