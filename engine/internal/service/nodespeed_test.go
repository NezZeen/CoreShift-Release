package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/store"
)

// shortNodeSpeed shortens the clocks of the speed test, for a test.
func shortNodeSpeed(t *testing.T, window, warm, firstByte time.Duration) {
	t.Helper()
	w, wm, fb := nodeSpeedWindow, nodeSpeedWarm, nodeSpeedFirstByte
	nodeSpeedWindow, nodeSpeedWarm, nodeSpeedFirstByte = window, warm, firstByte
	t.Cleanup(func() { nodeSpeedWindow, nodeSpeedWarm, nodeSpeedFirstByte = w, wm, fb })
}

// rateServer serves /down?bytes=N (the size is N, or all of the file when
// there is none): perTick bytes every tick, without a pause when tick is 0.
// Nothing is sent before first, which is how long a slow start takes; its
// pieces before that go at slow bytes a tick.
type rateServer struct {
	*httptest.Server
	tick, first   time.Duration
	perTick, slow int
	file          int64
	mu            sync.Mutex
	asked         []int64 // sizes asked, in order
	active, most  int
	started       chan struct{}
}

func newRateServer(t *testing.T, perTick int, tick time.Duration) *rateServer {
	t.Helper()
	rs := &rateServer{tick: tick, perTick: perTick, slow: perTick, file: 1 << 30, started: make(chan struct{}, 64)}
	rs.Server = httptest.NewServer(http.HandlerFunc(rs.serve))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *rateServer) serve(w http.ResponseWriter, r *http.Request) {
	size := rs.file
	if v := r.URL.Query().Get("bytes"); v != "" {
		size, _ = strconv.ParseInt(v, 10, 64)
	}
	rs.mu.Lock()
	rs.asked = append(rs.asked, size)
	rs.active++
	rs.most = max(rs.most, rs.active)
	rs.mu.Unlock()
	defer func() {
		rs.mu.Lock()
		rs.active--
		rs.mu.Unlock()
	}()
	select {
	case rs.started <- struct{}{}:
	default:
	}
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	begin := time.Now()
	chunk := make([]byte, rs.perTick)
	for sent := int64(0); sent < size; {
		n := rs.perTick
		if time.Since(begin) < rs.first {
			n = rs.slow
		}
		n = int(min(int64(n), size-sent))
		if _, err := w.Write(chunk[:n]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		sent += int64(n)
		if rs.tick > 0 {
			select {
			case <-time.After(rs.tick):
			case <-r.Context().Done():
				return
			}
		}
	}
}

func (rs *rateServer) sizes() []int64 {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]int64(nil), rs.asked...)
}

func plainClient() *http.Client { return &http.Client{Transport: &http.Transport{}} }

// A download's speed is the line's: 50 KB every 50 ms is 8 Mbit/s.
func TestDownloadSpeedMath(t *testing.T) {
	shortNodeSpeed(t, 1500*time.Millisecond, 300*time.Millisecond, 5*time.Second)
	rs := newRateServer(t, 50_000, 50*time.Millisecond)
	mbps, got, err := downloadSpeed(context.Background(), plainClient(), speedSource{URL: rs.URL + "/file"}, 100<<20)
	if err != nil {
		t.Fatal(err)
	}
	if mbps < 6.4 || mbps > 9.6 {
		t.Errorf("%.2f Mbit/s, want about 8", mbps)
	}
	// The window ends it: about 1.5 s of 1 MB/s, far from the cap.
	if got < 1_000_000 || got > 2_200_000 {
		t.Errorf("%d bytes in a window of 1.5 s at 1 MB/s", got)
	}
}

// The slow start is left out: a server that crawls for the first half
// second and then runs at 16 Mbit/s is a 16 Mbit/s server.
func TestDownloadSpeedLeavesOutTheStart(t *testing.T) {
	shortNodeSpeed(t, 2*time.Second, 600*time.Millisecond, 5*time.Second)
	rs := newRateServer(t, 100_000, 50*time.Millisecond)
	rs.first, rs.slow = 500*time.Millisecond, 20_000 // 3.2 Mbit/s at first
	mbps, _, err := downloadSpeed(context.Background(), plainClient(), speedSource{URL: rs.URL + "/file"}, 100<<20)
	if err != nil {
		t.Fatal(err)
	}
	// The whole download averages about 13 Mbit/s.
	if mbps < 14 || mbps > 19 {
		t.Errorf("%.2f Mbit/s, want about 16", mbps)
	}
}

// A download never takes more than the cap, however it is cut into pieces,
// and no piece is bigger than the source takes.
func TestDownloadSpeedByteCap(t *testing.T) {
	shortNodeSpeed(t, 5*time.Second, 100*time.Millisecond, 5*time.Second)

	rs := newRateServer(t, 64<<10, 0)
	_, got, err := downloadSpeed(context.Background(), plainClient(), speedSource{URL: rs.URL + "/file"}, 1<<20)
	if err != nil || got != 1<<20 {
		t.Errorf("one file: %d bytes, %v; want exactly the cap", got, err)
	}

	rs = newRateServer(t, 64<<10, 0)
	src := speedSource{URL: rs.URL + "/down?bytes=%d", Chunk: 300_000}
	_, got, err = downloadSpeed(context.Background(), plainClient(), src, 1<<20)
	if err != nil || got != 1<<20 {
		t.Errorf("pieces: %d bytes, %v; want exactly the cap", got, err)
	}
	sizes := rs.sizes()
	if len(sizes) != 4 {
		t.Errorf("pieces asked: %v, want 3 of 300000 and the rest", sizes)
	}
	for _, s := range sizes {
		if s > 300_000 {
			t.Errorf("a piece of %d bytes asked from a source that takes %d", s, 300_000)
		}
	}
}

// A slow server is cut off after the window, however little it sent.
func TestDownloadSpeedTimeCap(t *testing.T) {
	shortNodeSpeed(t, time.Second, 200*time.Millisecond, 5*time.Second)
	rs := newRateServer(t, 10_000, 100*time.Millisecond) // 0.8 Mbit/s
	begin := time.Now()
	mbps, got, err := downloadSpeed(context.Background(), plainClient(), speedSource{URL: rs.URL + "/file"}, 100<<20)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(begin); d > 2*time.Second {
		t.Errorf("took %v, the window is 1 s", d)
	}
	if got < 50_000 || got > 200_000 || mbps < 0.5 || mbps > 1.2 {
		t.Errorf("%d bytes, %.2f Mbit/s", got, mbps)
	}
}

// A source that sends nothing, or refuses, is an error, not a zero speed.
func TestDownloadSpeedFailures(t *testing.T) {
	shortNodeSpeed(t, time.Second, 100*time.Millisecond, 300*time.Millisecond)
	silent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer silent.Close()
	begin := time.Now()
	if _, got, err := downloadSpeed(context.Background(), plainClient(), speedSource{URL: silent.URL}, 1<<20); err == nil || got != 0 {
		t.Errorf("a silent source: %d bytes, %v", got, err)
	} else if !strings.Contains(err.Error(), "nothing came") || time.Since(begin) > 2*time.Second {
		t.Errorf("a silent source: %v after %v", err, time.Since(begin))
	}
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer limited.Close()
	if _, _, err := downloadSpeed(context.Background(), plainClient(), speedSource{URL: limited.URL}, 1<<20); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("a source that limits: %v", err)
	}
	// Stopping is the caller's, not a figure.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := downloadSpeed(ctx, plainClient(), speedSource{URL: silent.URL}, 1<<20); err == nil {
		t.Error("a cancelled download has a result")
	}
}

// speedRig is a store API whose test cores are the fake ones, which relay
// the name speed.test to the data server.
func speedRig(t *testing.T, rs *rateServer, sources []speedSource) (*harness, *httptest.Server) {
	t.Helper()
	u, _ := url.Parse(rs.URL)
	t.Setenv("FAKECORE_FORWARD", "speed.test="+u.Host)
	h, srv := newStoreAPI(t, func(c *Config) { c.speedSources = sources })
	return h, srv
}

var speedLinks = []string{
	"trojan://pw@203.0.113.6:443?sni=a.example.com#One",
	"trojan://pw@203.0.113.7:443?sni=a.example.com#Two",
	"trojan://pw@203.0.113.8:443?sni=a.example.com#Three",
}

func addSpeedLinks(t *testing.T, srv *httptest.Server, links []string) subscriptionView {
	t.Helper()
	var sub subscriptionView
	if code := callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": strings.Join(links, "\n")}, &sub); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("add subscription: %d", code)
	}
	return sub
}

// speedEvents collects the "nodespeed" events until stop is seen.
func waitSpeed(t *testing.T, h *harness, last ...string) []Event {
	t.Helper()
	var out []Event
	deadline := time.After(60 * time.Second)
	for {
		select {
		case e := <-h.events:
			if e.Kind != "nodespeed" {
				continue
			}
			out = append(out, e)
			if e.Reason == "done" || e.Reason == "stopped" {
				return out
			}
		case <-deadline:
			t.Fatalf("no end of the speed test; events so far: %+v", out)
		}
	}
}

func drain(h *harness) {
	for {
		select {
		case <-h.events:
		default:
			return
		}
	}
}

// The servers are tested one at a time, each through its own core, and the
// results come as events, in the views and on disk.
func TestNodeSpeedTestsEachServerThroughItsCore(t *testing.T) {
	shortNodeSpeed(t, time.Second, 100*time.Millisecond, 5*time.Second)
	rs := newRateServer(t, 64<<10, 0)
	h, srv := speedRig(t, rs, []speedSource{{Name: "fake", URL: "http://speed.test/down?bytes=%d", Chunk: 256 << 10}})
	sub := addSpeedLinks(t, srv, speedLinks)
	drain(h)

	var start NodeSpeedStart
	code := callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{"subscription": sub.ID, "bytes": 1 << 20}, &start)
	if code != http.StatusAccepted || start.Total != 3 || start.Bytes != 1<<20 || start.Estimate != 3<<20 {
		t.Fatalf("start: %d %+v", code, start)
	}
	events := waitSpeed(t, h)

	if first := events[0]; first.Reason != "started" || first.Total != 3 || first.Bytes != 1<<20 {
		t.Errorf("first event %+v", first)
	}
	var nodes []Event
	for _, e := range events {
		if e.Reason == "node" {
			nodes = append(nodes, e)
		}
	}
	if len(nodes) != 3 {
		t.Fatalf("%d node events: %+v", len(nodes), events)
	}
	for i, e := range nodes {
		if e.Fingerprint != sub.Nodes[i].Fingerprint || e.Mbps <= 0 || e.Bytes != 1<<20 || e.Error != "" || e.Source != "fake" || e.Core == "" {
			t.Errorf("node %d: %+v", i, e)
		}
	}
	if end := events[len(events)-1]; end.Reason != "done" || end.Bytes != 3<<20 || end.Total != 3 {
		t.Errorf("last event %+v: the volume used is the three servers' megabytes", end)
	}
	// One at a time: the data server never had two downloads at once.
	if rs.most != 1 {
		t.Errorf("%d downloads at once, want 1", rs.most)
	}
	// 1 MB in pieces of 256 KB, for each of the three.
	if sizes := rs.sizes(); len(sizes) != 12 {
		t.Errorf("pieces asked: %v", sizes)
	}

	// The results are with the servers, with the time they were measured.
	var got subscriptionView
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &got)
	for i, n := range got.Nodes {
		if n.SpeedMbps <= 0 || n.SpeedBytes != 1<<20 || n.SpeedError != "" || n.SpeedSource != "fake" || time.Since(n.SpeedAt) > time.Minute || n.SpeedAt.IsZero() {
			t.Errorf("node view %d: %+v", i, n)
		}
	}
	var st NodeSpeedStatus
	callJSON(t, srv, "GET", "/v1/speedtest/nodes", nil, &st)
	if st.Running || st.Done != 3 || st.Total != 3 || st.Bytes != 3<<20 {
		t.Errorf("status %+v", st)
	}

	// And they outlast the engine: a new one on the same data finds them.
	again := &Service{cfg: Config{DataDir: h.svc.cfg.DataDir}}
	if r, ok := again.NodeSpeed(sub.ID, sub.Nodes[1].Fingerprint); !ok || r.Mbps != got.Nodes[1].SpeedMbps || !r.TestedAt.Equal(got.Nodes[1].SpeedAt) {
		t.Errorf("after a restart: %+v %v", r, ok)
	}
	if b, err := os.ReadFile(filepath.Join(h.svc.cfg.DataDir, "nodespeed.json")); err != nil || strings.Contains(string(b), "pw") {
		t.Errorf("nodespeed.json: %v", err)
	}
}

// Only the servers asked for are tested, and a source that gives nothing
// gives way to the next.
func TestNodeSpeedFallsBackAndSelects(t *testing.T) {
	shortNodeSpeed(t, time.Second, 100*time.Millisecond, 2*time.Second)
	rs := newRateServer(t, 64<<10, 0)
	h, srv := speedRig(t, rs, []speedSource{
		// Not the zone the fake core relays: it answers 204 to anything.
		{Name: "broken", URL: "http://other.test/down?bytes=%d", Chunk: 1 << 20},
		{Name: "second", URL: "http://speed.test/file"},
	})
	sub := addSpeedLinks(t, srv, speedLinks)
	drain(h)

	pick := sub.Nodes[1].Fingerprint
	if code := callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{"subscription": sub.ID, "nodes": []string{pick}, "bytes": 1 << 20}, nil); code != http.StatusAccepted {
		t.Fatalf("start: %d", code)
	}
	events := waitSpeed(t, h)
	var nodes []Event
	for _, e := range events {
		if e.Reason == "node" {
			nodes = append(nodes, e)
		}
	}
	if len(nodes) != 1 || nodes[0].Fingerprint != pick || nodes[0].Source != "second" || nodes[0].Mbps <= 0 || nodes[0].Bytes != 1<<20 {
		t.Fatalf("node events: %+v", nodes)
	}
	var got subscriptionView
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &got)
	for i, n := range got.Nodes {
		if (n.SpeedMbps > 0) != (n.Fingerprint == pick) || (i != 1 && !n.SpeedAt.IsZero()) {
			t.Errorf("node view %d: %+v", i, n)
		}
	}
}

// A server through which nothing downloads fails with a code the app words.
func TestNodeSpeedFailureHasACode(t *testing.T) {
	shortNodeSpeed(t, time.Second, 100*time.Millisecond, 2*time.Second)
	rs := newRateServer(t, 64<<10, 0)
	h, srv := speedRig(t, rs, []speedSource{{Name: "broken", URL: "http://other.test/file"}})
	sub := addSpeedLinks(t, srv, speedLinks[:1])
	drain(h)
	callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{"bytes": 1 << 20}, nil)
	events := waitSpeed(t, h)
	var node Event
	for _, e := range events {
		if e.Reason == "node" {
			node = e
		}
	}
	if node.Mbps != 0 || node.Code != "speed.nodata" || !strings.Contains(node.Error, "ничего не скачалось") || !strings.Contains(node.Error, "broken") {
		t.Errorf("failed node: %+v", node)
	}
	if events[len(events)-1].Reason != "done" || events[len(events)-1].Bytes != 0 {
		t.Errorf("end: %+v", events[len(events)-1])
	}
	var got subscriptionView
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &got)
	if n := got.Nodes[0]; n.SpeedMbps != 0 || n.SpeedErrorCode != "speed.nodata" || n.SpeedError == "" || n.SpeedAt.IsZero() {
		t.Errorf("node view: %+v", n)
	}
}

// Stopping ends the test at once and keeps the servers already measured; the
// server in the middle has no figure, and no core is left running.
func TestNodeSpeedStop(t *testing.T) {
	shortNodeSpeed(t, 20*time.Second, 100*time.Millisecond, 5*time.Second)
	rs := newRateServer(t, 10_000, 100*time.Millisecond)
	h, srv := speedRig(t, rs, []speedSource{{Name: "slow", URL: "http://speed.test/file"}})
	sub := addSpeedLinks(t, srv, speedLinks)
	drain(h)
	callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{"subscription": sub.ID, "bytes": 1 << 20}, nil)
	// A second test cannot begin meanwhile.
	if code := callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{}, nil); code != http.StatusConflict {
		t.Errorf("a second test: %d", code)
	}
	select {
	case <-rs.started:
	case <-time.After(20 * time.Second):
		t.Fatal("no download began")
	}
	time.Sleep(300 * time.Millisecond)
	begin := time.Now()
	var st NodeSpeedStatus
	if code := callJSON(t, srv, "DELETE", "/v1/speedtest/nodes", nil, &st); code != http.StatusOK {
		t.Fatalf("stop: %d", code)
	}
	events := waitSpeed(t, h)
	if d := time.Since(begin); d > 5*time.Second {
		t.Errorf("stopping took %v", d)
	}
	end := events[len(events)-1]
	if end.Reason != "stopped" || end.Bytes <= 0 || end.Bytes >= 1<<20 {
		t.Errorf("end %+v: the few bytes of the stopped server are counted", end)
	}
	for _, e := range events {
		if e.Reason == "node" {
			t.Errorf("a result from a stopped test: %+v", e)
		}
	}
	var got subscriptionView
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &got)
	for _, n := range got.Nodes {
		if !n.SpeedAt.IsZero() {
			t.Errorf("a figure from a stopped server: %+v", n)
		}
	}
	callJSON(t, srv, "GET", "/v1/speedtest/nodes", nil, &st)
	if st.Running {
		t.Errorf("still running: %+v", st)
	}
	// Another test may begin.
	if code := callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{"bytes": 1 << 20}, nil); code != http.StatusAccepted {
		t.Errorf("a test after the stop: %d", code)
	}
	callJSON(t, srv, "DELETE", "/v1/speedtest/nodes", nil, nil)
	waitSpeed(t, h)
}

// Connecting ends the test: its downloads would compete with the connection.
func TestNodeSpeedStopsWhenConnecting(t *testing.T) {
	shortNodeSpeed(t, 20*time.Second, 100*time.Millisecond, 5*time.Second)
	rs := newRateServer(t, 10_000, 100*time.Millisecond)
	h, srv := speedRig(t, rs, []speedSource{{Name: "slow", URL: "http://speed.test/file"}})
	addSpeedLinks(t, srv, speedLinks)
	drain(h)
	callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{"bytes": 1 << 20}, nil)
	select {
	case <-rs.started:
	case <-time.After(20 * time.Second):
		t.Fatal("no download began")
	}
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	events := waitSpeed(t, h)
	if events[len(events)-1].Reason != "stopped" {
		t.Errorf("end %+v", events[len(events)-1])
	}
}

// The test is turned away when its request is wrong, or when the other
// test of the list is on.
func TestNodeSpeedRequests(t *testing.T) {
	rs := newRateServer(t, 64<<10, 0)
	h, srv := speedRig(t, rs, []speedSource{{Name: "fake", URL: "http://speed.test/file"}})
	sub := addSpeedLinks(t, srv, speedLinks[:1])
	for name, body := range map[string]map[string]any{
		"too little":      {"bytes": 1000},
		"too much":        {"bytes": 1 << 40},
		"no such server":  {"nodes": []string{"nope"}},
		"no subscription": {"subscription": "nope"},
	} {
		if code := callJSON(t, srv, "POST", "/v1/speedtest/nodes", body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	// Without a volume the setting says how much.
	set := h.svc.Store().Settings()
	if set.Cores.SpeedMB != store.DefaultSpeedMB {
		t.Fatalf("default volume %d MB", set.Cores.SpeedMB)
	}
	set.Cores.SpeedMB = 5
	if code := callJSON(t, srv, "PUT", "/v1/settings", set, nil); code != http.StatusOK {
		t.Fatalf("settings: %d", code)
	}
	var start NodeSpeedStart
	if code := callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{"subscription": sub.ID}, &start); code != http.StatusAccepted || start.Bytes != 5<<20 || start.Estimate != 5<<20 {
		t.Errorf("start by the setting: %d %+v", code, start)
	}
	waitSpeed(t, h)

	set.Cores.SpeedMB = 7
	if code := callJSON(t, srv, "PUT", "/v1/settings", set, nil); code != http.StatusBadRequest {
		t.Errorf("a volume that is not one of the choices: %d", code)
	}

	// A latency test in progress: the speed test waits.
	h.svc.latency.mu.Lock()
	h.svc.latency.running = true
	h.svc.latency.mu.Unlock()
	if code := callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{}, nil); code != http.StatusConflict {
		t.Errorf("during a latency test: %d", code)
	}
	h.svc.latency.mu.Lock()
	h.svc.latency.running = false
	h.svc.latency.mu.Unlock()
}

// Mobile data is told, so that the app can warn about the traffic.
func TestNodeSpeedStatusKnowsMobileData(t *testing.T) {
	name := atomic.Value{}
	name.Store("wlan0")
	h, srv := newStoreAPI(t, func(c *Config) { c.netName = func() string { return name.Load().(string) } })
	_ = h
	for iface, want := range map[string]bool{"wlan0": false, "rmnet_data2": true, "ccmni1": true, "eth0": false} {
		name.Store(iface)
		var st NodeSpeedStatus
		callJSON(t, srv, "GET", "/v1/speedtest/nodes", nil, &st)
		if st.Mobile != want {
			t.Errorf("%s: mobile = %v", iface, st.Mobile)
		}
	}
}

// The endpoints are behind the token and the Host check, like the rest.
func TestNodeSpeedAPIRejectsStrangers(t *testing.T) {
	_, srv := newStoreAPI(t, nil)
	_, port, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	for _, m := range []string{"POST", "GET", "DELETE"} {
		call := func(mutate func(*http.Request)) int {
			req, _ := http.NewRequest(m, srv.URL+"/v1/speedtest/nodes", strings.NewReader("{}"))
			mutate(req)
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			return resp.StatusCode
		}
		if code := call(func(r *http.Request) {}); code != http.StatusUnauthorized {
			t.Errorf("%s without a token: %d", m, code)
		}
		if code := call(func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }); code != http.StatusUnauthorized {
			t.Errorf("%s with a wrong token: %d", m, code)
		}
		if code := call(func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
			r.Host = "evil.example:" + port
		}); code != http.StatusForbidden {
			t.Errorf("%s from a foreign host: %d", m, code)
		}
	}
}

// No subscription URL or password gets into an event or a result.
func TestNodeSpeedEventsCarryNoSecrets(t *testing.T) {
	shortNodeSpeed(t, time.Second, 100*time.Millisecond, 2*time.Second)
	rs := newRateServer(t, 64<<10, 0)
	h, srv := speedRig(t, rs, []speedSource{{Name: "fake", URL: "http://speed.test/file"}})
	addSpeedLinks(t, srv, speedLinks[:1])
	drain(h)
	callJSON(t, srv, "POST", "/v1/speedtest/nodes", map[string]any{"bytes": 1 << 20}, nil)
	for _, e := range waitSpeed(t, h) {
		b, _ := json.Marshal(e)
		if strings.Contains(string(b), "socks5://") || strings.Contains(string(b), "pw") {
			t.Errorf("event %s", b)
		}
	}
}
