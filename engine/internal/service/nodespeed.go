package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/node"
)

// The download speed of the servers in the list, one after another, as
// Throne's speed test does: a test core for the server, a download through
// it for a few seconds or a few megabytes, whichever comes first, the core
// stopped. One server at a time, so that they do not compete for the line.

var (
	// ErrNodeSpeedRunning means a speed test of the servers is in progress.
	ErrNodeSpeedRunning = errors.New("a server speed test is already running")
	// ErrNoNodesToTest means the request names no server that is in the list.
	ErrNoNodesToTest = errors.New("no servers to test")
)

const (
	// Bounds of the volume one server may download in a request.
	nodeSpeedMinBytes = 1 << 20
	nodeSpeedMaxBytes = 100 << 20
	// nodeSpeedMinSteady is the least time a figure is worth taking from.
	nodeSpeedMinSteady = 250 * time.Millisecond
	// nodeSpeedKeep is how long a result is kept: an old figure only
	// misleads.
	nodeSpeedKeep = 30 * 24 * time.Hour
)

// How long a server downloads; tests shorten these.
var (
	// nodeSpeedWindow is the longest a download goes on, from the first byte.
	nodeSpeedWindow = 5 * time.Second
	// nodeSpeedWarm is what is left out of the figure at the start: TCP slow
	// start would make every server look slow.
	nodeSpeedWarm = 500 * time.Millisecond
	// nodeSpeedFirstByte is how long a source has to start sending.
	nodeSpeedFirstByte = 10 * time.Second
)

// speedSource is a file to download to measure a server's speed.
type speedSource struct {
	Name string
	// URL of the file. For a source that serves any size (Chunk > 0) it
	// has %d for the size asked.
	URL string
	// Chunk is the biggest piece asked at once from such a source:
	// Cloudflare turns away a download of more than about 10 MB with
	// "429 Too Many Requests" for half an hour (speedtest.go), so a bigger
	// volume comes as several pieces over one connection.
	Chunk int64
}

// nodeSpeedSources are tried in order; the next one only when the one
// before sent nothing through the server.
var nodeSpeedSources = []speedSource{
	{Name: "cloudflare", URL: speedServer + "/__down?bytes=%d", Chunk: 8 << 20},
	{Name: "cachefly", URL: "http://cachefly.cachefly.net/100mb.test"},
	{Name: "hetzner", URL: "https://speed.hetzner.de/100MB.bin"},
}

// NodeSpeed is the last speed test of one node.
type NodeSpeed struct {
	Subscription string `json:"subscription"`
	Fingerprint  string `json:"fingerprint"`
	Core         string `json:"core,omitempty"`
	// Source is the name of the file that was downloaded.
	Source string `json:"source,omitempty"`
	// Mbps is the download speed in megabits per second (10^6), zero for a
	// server that failed.
	Mbps float64 `json:"mbps,omitempty"`
	// Bytes is what the test downloaded through the server.
	Bytes int64 `json:"bytes,omitempty"`
	// Error says why nothing was measured; ErrorCode and ErrorArgs say it
	// in the app's language (internal/msg).
	Error     string         `json:"error,omitempty"`
	ErrorCode string         `json:"error_code,omitempty"`
	ErrorArgs map[string]any `json:"error_args,omitempty"`
	TestedAt  time.Time      `json:"tested_at"`
}

// NodeSpeedRequest says which servers to test and how much each may download.
type NodeSpeedRequest struct {
	// Subscription limits the test to one subscription, empty for all.
	Subscription string `json:"subscription"`
	// Nodes limits it to these fingerprints, empty for every server.
	Nodes []string `json:"nodes"`
	// Bytes is the most one server downloads; zero means the setting
	// cores.speed_mb.
	Bytes int64 `json:"bytes"`
}

// NodeSpeedStart is what a started test tells at once.
type NodeSpeedStart struct {
	Total    int   `json:"total"`
	Bytes    int64 `json:"bytes"`
	Estimate int64 `json:"estimate"`
}

// NodeSpeedStatus is how the test of the servers stands.
type NodeSpeedStatus struct {
	Running bool `json:"running"`
	// Done of Total servers are through; Bytes were downloaded so far.
	Done  int   `json:"done"`
	Total int   `json:"total"`
	Bytes int64 `json:"bytes"`
	// BytesPerNode is the cap the running test has.
	BytesPerNode int64 `json:"bytes_per_node,omitempty"`
	// Mobile means the device is on mobile data, where a test costs money.
	Mobile bool `json:"mobile"`
}

type nodeSpeedState struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	loaded  bool
	results map[string]NodeSpeed // by subscription + "/" + fingerprint
	done    int
	total   int
	bytes   int64
	perNode int64
}

func (s *Service) nodeSpeedPath() string {
	if s.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.DataDir, "nodespeed.json")
}

// loadLocked reads the results of earlier runs, once. l.mu is held.
func (s *Service) loadNodeSpeedLocked() {
	l := &s.nodeSpeed
	if l.loaded {
		return
	}
	l.loaded = true
	l.results = map[string]NodeSpeed{}
	path := s.nodeSpeedPath()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]NodeSpeed
	if json.Unmarshal(b, &saved) != nil {
		return
	}
	for k, r := range saved {
		if time.Since(r.TestedAt) < nodeSpeedKeep {
			l.results[k] = r
		}
	}
}

// saveNodeSpeedLocked writes the results down, so that the list shows
// them after the app, or on a phone the engine, restarts. l.mu is held.
func (s *Service) saveNodeSpeedLocked() {
	path := s.nodeSpeedPath()
	if path == "" {
		return
	}
	b, err := json.Marshal(s.nodeSpeed.results)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		os.Remove(tmp)
	}
}

// NodeSpeed returns the last speed test of a node, if it was tested.
func (s *Service) NodeSpeed(subID, fingerprint string) (NodeSpeed, bool) {
	l := &s.nodeSpeed
	l.mu.Lock()
	defer l.mu.Unlock()
	s.loadNodeSpeedLocked()
	r, ok := l.results[subID+"/"+fingerprint]
	return r, ok
}

// stopNodeSpeed ends a running speed test of the servers, keeping the
// results so far: connecting should not wait for it, and its downloads
// would compete with the connection's.
func (s *Service) stopNodeSpeed() {
	l := &s.nodeSpeed
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
	}
}

// NodeSpeedStatus tells how the test of the servers stands.
func (s *Service) NodeSpeedStatus() NodeSpeedStatus {
	l := &s.nodeSpeed
	l.mu.Lock()
	st := NodeSpeedStatus{Running: l.running, Done: l.done, Total: l.total, Bytes: l.bytes, BytesPerNode: l.perNode}
	l.mu.Unlock()
	if s.cfg.netName != nil {
		st.Mobile = networkKind(s.cfg.netName()).Code == "net.kind.mobile"
	}
	return st
}

// StartNodeSpeed starts the speed test of the servers req names and returns
// at once; the results arrive as "nodespeed" events: Reason "started" (Total
// servers, Bytes the cap of each), then "node" for each server (Fingerprint,
// Mbps, Bytes, Source, or Error), then "done" with Bytes, all that was
// downloaded, or "stopped" when it was ended early (StopNodeSpeed, a
// connection being made). The results are kept with the servers (NodeSpeed).
func (s *Service) StartNodeSpeed(req NodeSpeedRequest) (NodeSpeedStart, error) {
	st := s.cfg.Store
	if st == nil {
		return NodeSpeedStart{}, errors.New("no store")
	}
	limit := req.Bytes
	if limit == 0 {
		limit = int64(st.Settings().Cores.SpeedMB) << 20
	}
	if limit < nodeSpeedMinBytes || limit > nodeSpeedMaxBytes {
		return NodeSpeedStart{}, fmt.Errorf("bytes: %d is outside %d to %d", limit, nodeSpeedMinBytes, nodeSpeedMaxBytes)
	}

	type ref struct{ sub, fp string }
	var nodes []node.Node
	var refs []ref
	known := map[string]bool{}
	for _, sub := range st.Subscriptions() {
		if req.Subscription != "" && sub.ID != req.Subscription {
			continue
		}
		fps := sub.Fingerprints()
		for i, n := range sub.Nodes {
			if sub.IsHidden(fps[i]) {
				continue // removed from the list by the user
			}
			known[sub.ID+"/"+fps[i]] = true
			if len(req.Nodes) > 0 && !slices.Contains(req.Nodes, fps[i]) {
				continue
			}
			nodes = append(nodes, n)
			refs = append(refs, ref{sub.ID, fps[i]})
		}
	}
	if req.Subscription != "" && len(known) == 0 {
		if _, ok := st.Subscription(req.Subscription); !ok {
			return NodeSpeedStart{}, errors.New("no such subscription")
		}
	}
	if len(nodes) == 0 {
		return NodeSpeedStart{}, ErrNoNodesToTest
	}

	// The two tests would compete for the line and spoil each other.
	s.latency.mu.Lock()
	busy := s.latency.running
	s.latency.mu.Unlock()
	if busy {
		return NodeSpeedStart{}, ErrTestRunning
	}

	l := &s.nodeSpeed
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return NodeSpeedStart{}, ErrNodeSpeedRunning
	}
	s.loadNodeSpeedLocked()
	// Servers that left the subscriptions take their results along.
	for k := range l.results {
		if (req.Subscription == "" || strings.HasPrefix(k, req.Subscription+"/")) && !known[k] {
			delete(l.results, k)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.running, l.cancel = true, cancel
	l.done, l.total, l.bytes, l.perNode = 0, len(nodes), 0, limit
	l.mu.Unlock()

	// Told before the test begins, so that no event of it comes first.
	s.hub.publish(Event{Kind: "nodespeed", Reason: "started", Subscription: req.Subscription, Total: len(nodes), Bytes: limit})
	go func() {
		defer cancel()
		fps := make([]string, len(refs))
		subs := make([]string, len(refs))
		for i, r := range refs {
			subs[i], fps[i] = r.sub, r.fp
		}
		s.runNodeSpeed(ctx, nodes, subs, fps, limit, req.Subscription)
	}()
	return NodeSpeedStart{Total: len(nodes), Bytes: limit, Estimate: limit * int64(len(nodes))}, nil
}

// StopNodeSpeed ends the running test early.
func (s *Service) StopNodeSpeed() { s.stopNodeSpeed() }

func (s *Service) runNodeSpeed(ctx context.Context, nodes []node.Node, subs, fps []string, limit int64, subID string) {
	l := &s.nodeSpeed
	stopped := false
	defer func() {
		l.mu.Lock()
		total := l.bytes
		l.running, l.cancel = false, nil
		l.mu.Unlock()
		reason := "done"
		if stopped {
			reason = "stopped"
		}
		s.hub.publish(Event{Kind: "nodespeed", Reason: reason, Subscription: subID, Bytes: total, Total: len(nodes)})
	}()

	names := s.serverNames(ctx)
	addrs := s.resolveServers(ctx, nodes, names)
	sources := s.cfg.speedSources
	if sources == nil {
		sources = nodeSpeedSources
	}
	for i := range nodes {
		if ctx.Err() != nil {
			break
		}
		s.hub.publish(Event{Kind: "nodespeed", Reason: "testing", Subscription: subs[i], Fingerprint: fps[i]})
		res := s.measureNodeSpeed(ctx, &nodes[i], addrs[i], limit, sources)
		l.mu.Lock()
		l.bytes += res.Bytes // what a stopped server downloaded was used too
		l.mu.Unlock()
		if ctx.Err() != nil {
			break // stopped in the middle of this one: its figure is not whole
		}
		res.Subscription, res.Fingerprint, res.TestedAt = subs[i], fps[i], time.Now()
		l.mu.Lock()
		l.results[res.Subscription+"/"+res.Fingerprint] = res
		l.done++
		s.saveNodeSpeedLocked()
		l.mu.Unlock()
		e := Event{Kind: "nodespeed", Reason: "node", Subscription: res.Subscription, Fingerprint: res.Fingerprint,
			Core: res.Core, Source: res.Source, Mbps: res.Mbps, Bytes: res.Bytes}
		if res.Error != "" {
			e.Error, e.Code, e.Args = res.Error, res.ErrorCode, res.ErrorArgs
		}
		s.hub.publish(e)
	}
	stopped = ctx.Err() != nil
}

// measureNodeSpeed downloads through n's test core from the first of
// sources that sends anything.
func (s *Service) measureNodeSpeed(ctx context.Context, n *node.Node, serverAddr string, limit int64, sources []speedSource) NodeSpeed {
	var res NodeSpeed
	k, err := s.sup.WithNodeProxy(ctx, n, serverAddr, func(ctx context.Context, proxy *url.URL) error {
		tr := newTransport(proxy)
		tr.DisableCompression = true
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr}
		var errs []error
		for _, src := range sources {
			if ctx.Err() != nil {
				break
			}
			mbps, got, err := downloadSpeed(ctx, client, src, limit)
			res.Bytes += got
			if err == nil {
				res.Mbps, res.Source = mbps, src.Name
				return nil
			}
			errs = append(errs, fmt.Errorf("%s: %w", src.Name, err))
		}
		if len(errs) == 0 {
			return ctx.Err()
		}
		return errors.Join(errs...)
	})
	res.Core = string(k)
	if err != nil {
		m := msg.New("speed.nodata", "err", err)
		res.Error, res.ErrorCode, res.ErrorArgs = m.String(), m.Code, m.Args
	}
	return res
}

// downloadSpeed downloads from src through client, at most limit bytes and
// for at most nodeSpeedWindow after the first byte, and returns the speed in
// megabits per second and the bytes received.
//
// The figure leaves out the start, which TCP slow start makes slow
// (nodeSpeedWarm), and the time a next piece of a chunked source takes to
// start coming: it counts the time bytes were arriving and nothing else,
// the way a long download shows its speed. When the download is over
// before the start is, it is the whole of it.
func downloadSpeed(ctx context.Context, client *http.Client, src speedSource, limit int64) (float64, int64, error) {
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var m meter
	began := time.Now()
	firstTimer := time.AfterFunc(nodeSpeedFirstByte, cancel)
	defer firstTimer.Stop()
	var window *time.Timer
	defer func() {
		if window != nil {
			window.Stop()
		}
	}()

	buf := make([]byte, 32<<10)
	var failure error
loop:
	for m.total < limit {
		size := limit - m.total
		target := src.URL
		if src.Chunk > 0 {
			size = min(size, src.Chunk)
			target = fmt.Sprintf(src.URL, size)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			failure = err
			break
		}
		resp, err := client.Do(req)
		if err != nil {
			failure = err
			break
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			failure = errors.New(resp.Status)
			break
		}
		body := io.LimitReader(resp.Body, size)
		var last time.Time
		for {
			k, err := body.Read(buf)
			now := time.Now()
			if k > 0 {
				if m.total == 0 {
					firstTimer.Stop()
					window = time.AfterFunc(nodeSpeedWindow, cancel)
				}
				m.add(int64(k), now, last)
				last = now
			}
			if err != nil {
				resp.Body.Close()
				if !errors.Is(err, io.EOF) {
					failure = err
					break loop
				}
				break
			}
		}
	}

	if m.total == 0 {
		if failure == nil {
			failure = errors.New("nothing came")
		}
		if err := parent.Err(); err != nil {
			failure = err
		} else if errors.Is(failure, context.Canceled) {
			failure = fmt.Errorf("nothing came in %d s", int(nodeSpeedFirstByte.Seconds()))
		}
		return 0, 0, failure
	}
	return m.mbps(began), m.total, nil
}

// meter works out a download's speed from the reads that make it.
type meter struct {
	total int64
	// timed: what came after the first read of each piece, and the time it
	// took; steady: of that, what came after the start was left out.
	timedBytes, steadyBytes int64
	timedDur, steadyDur     time.Duration
}

// add notes a read of k bytes at now; last is when the read before it, of
// the same piece, was, zero for the first of a piece.
func (m *meter) add(k int64, now, last time.Time) {
	m.total += k
	if last.IsZero() {
		return // the wait for the piece to start is not the line's speed
	}
	dt := now.Sub(last)
	if m.timedDur >= nodeSpeedWarm {
		m.steadyBytes += k
		m.steadyDur += dt
	}
	m.timedBytes += k
	m.timedDur += dt
}

func (m *meter) mbps(began time.Time) float64 {
	const mega = 1e6
	switch {
	case m.steadyDur >= nodeSpeedMinSteady:
		return float64(m.steadyBytes) * 8 / m.steadyDur.Seconds() / mega
	case m.timedDur > 0:
		return float64(m.timedBytes) * 8 / m.timedDur.Seconds() / mega
	}
	// Everything came in one read.
	return float64(m.total) * 8 / max(time.Since(began), time.Millisecond).Seconds() / mega
}
