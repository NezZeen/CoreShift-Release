package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOokla stands for speedtest.net: Pick answers with server or pickErr,
// each phase reports its rate, moves its bytes and lasts its d, unless
// block holds the download until the test ends.
type fakeOokla struct {
	server             ooklaServer
	pickErr            error
	downRate, upRate   int64
	downMoved, upMoved int64
	downErr, upErr     error
	block              bool

	proxy  atomic.Pointer[url.URL]
	rate   atomic.Int64
	moved  atomic.Int64
	closed atomic.Bool
}

// noOokla is speedtest.net out of reach.
func noOokla(*url.URL) ooklaTest {
	return &fakeOokla{pickErr: errors.New("dial tcp: lookup www.speedtest.net: no such host")}
}

func (f *fakeOokla) factory(proxy *url.URL) ooklaTest {
	if proxy != nil {
		f.proxy.Store(proxy)
	}
	return f
}

func (f *fakeOokla) Pick(context.Context) (ooklaServer, error) { return f.server, f.pickErr }

func (f *fakeOokla) phase(ctx context.Context, d time.Duration, rate, moved int64, err error, block bool) (int64, error) {
	f.rate.Store(rate)
	f.moved.Store(moved)
	if block {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	select {
	case <-time.After(d):
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	return rate, err
}

func (f *fakeOokla) Download(ctx context.Context, d time.Duration) (int64, error) {
	return f.phase(ctx, d, f.downRate, f.downMoved, f.downErr, f.block)
}

func (f *fakeOokla) Upload(ctx context.Context, d time.Duration) (int64, error) {
	return f.phase(ctx, d, f.upRate, f.upMoved, f.upErr, false)
}

func (f *fakeOokla) Rate() int64  { return f.rate.Load() }
func (f *fakeOokla) Moved() int64 { return f.moved.Load() }
func (f *fakeOokla) Close()       { f.closed.Store(true) }

var helsinki = ooklaServer{Name: "Helsinki", Sponsor: "Elisa", Country: "Finland", Latency: 23 * time.Millisecond}

// speedEvents drains the speed test's events.
func speedEvents(h *harness) []Event {
	var out []Event
	for len(h.events) > 0 {
		if e := <-h.events; e.Kind == "speedtest" {
			out = append(out, e)
		}
	}
	return out
}

// countingServer counts the requests that reach it.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { n.Add(1) }))
	t.Cleanup(srv.Close)
	return srv, &n
}

// A speedtest.net server measures, and Cloudflare is not asked.
func TestSpeedTestOokla(t *testing.T) {
	shortSpeedTest(t)
	f := &fakeOokla{server: helsinki, downRate: 12_500_000, upRate: 5_000_000, downMoved: 90 << 20, upMoved: 30 << 20}
	cf, cloudflare := countingServer(t)
	h := newHarness(t, func(c *Config) { c.speedURL, c.ookla = cf.URL, f.factory })

	res, err := h.svc.SpeedTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := SpeedResult{DownloadBps: 12_500_000, UploadBps: 5_000_000, LatencyMS: 23, TestServer: "Elisa", TestCity: "Helsinki", TestCountry: "Finland"}
	if res != want {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	if cloudflare.Load() != 0 {
		t.Errorf("Cloudflare was asked %d times", cloudflare.Load())
	}
	if f.proxy.Load() != nil {
		t.Errorf("went through a proxy while disconnected")
	}
	if !f.closed.Load() {
		t.Error("the test's connections were not let go")
	}

	// The live rates are the library's smoothed ones, phase by phase.
	var reasons []string
	for _, e := range speedEvents(h) {
		if len(reasons) == 0 || reasons[len(reasons)-1] != e.Reason {
			reasons = append(reasons, e.Reason)
		}
		switch e.Reason {
		case "latency":
			if e.LatencyMS != 23 {
				t.Errorf("latency event = %+v", e)
			}
		case "download":
			if e.DownRate != 12_500_000 || e.UpRate != 0 {
				t.Errorf("download event = %+v", e)
			}
		case "upload":
			if e.UpRate != 5_000_000 || e.DownRate != 0 {
				t.Errorf("upload event = %+v", e)
			}
		case "done":
			if e.DownRate != res.DownloadBps || e.UpRate != res.UploadBps {
				t.Errorf("done event = %+v", e)
			}
		}
	}
	if !slices.Equal(reasons, []string{"latency", "download", "upload", "done"}) {
		t.Errorf("events = %v", reasons)
	}
}

// Without a rate from the library, the phase's own average stands in.
func TestSpeedTestOoklaRateFromBytes(t *testing.T) {
	shortSpeedTest(t)
	f := &fakeOokla{server: helsinki, downMoved: 3 << 20, upMoved: 1 << 20}
	h := newHarness(t, func(c *Config) { c.ookla = f.factory })
	res, err := h.svc.SpeedTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.DownloadBps <= 0 || res.UploadBps <= 0 || res.DownloadBps <= res.UploadBps {
		t.Errorf("result = %+v", res)
	}
}

// When speedtest.net cannot be used before anything was measured, the
// test goes to Cloudflare without a word, and says so in the result.
func TestSpeedTestFallsBackToCloudflare(t *testing.T) {
	for name, f := range map[string]*fakeOokla{
		"no server list": {pickErr: errors.New(`Get "https://www.speedtest.net/api/js/servers": EOF`)},
		"no server":      {pickErr: errNoOoklaServer},
		"nothing moved":  {server: helsinki, downErr: errors.New("connection reset")},
		"nothing at all": {server: helsinki},
	} {
		t.Run(name, func(t *testing.T) {
			shortSpeedTest(t)
			srv := speedServerFake(t, nil)
			h := newHarness(t, func(c *Config) { c.speedURL, c.ookla = srv.URL, f.factory })
			res, err := h.svc.SpeedTest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if res.TestServer != "Cloudflare" || res.TestCity != "" || res.TestCountry != "" || res.DownloadBps <= 0 || res.UploadBps <= 0 {
				t.Errorf("result = %+v", res)
			}
			for _, e := range speedEvents(h) {
				if e.Reason == "error" {
					t.Errorf("the fallback showed an error: %+v", e)
				}
			}
			if !f.closed.Load() {
				t.Error("the speedtest.net test was not closed")
			}
		})
	}
}

// Once speedtest.net has measured the download, a failed upload is the
// test's error: the figures would otherwise mix two servers.
func TestSpeedTestOoklaUploadFails(t *testing.T) {
	shortSpeedTest(t)
	f := &fakeOokla{server: helsinki, downRate: 1 << 20, downMoved: 1 << 20, upErr: errors.New("upload request failed: 403 Forbidden")}
	cf, cloudflare := countingServer(t)
	h := newHarness(t, func(c *Config) { c.speedURL, c.ookla = cf.URL, f.factory })
	_, err := h.svc.SpeedTest(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "upload: ") || errors.Is(err, errOoklaUnusable) {
		t.Fatalf("err = %v", err)
	}
	if cloudflare.Load() != 0 {
		t.Errorf("Cloudflare was asked %d times", cloudflare.Load())
	}
}

// A cancelled test stops at once and does not go on to Cloudflare.
func TestSpeedTestOoklaCancel(t *testing.T) {
	f := &fakeOokla{server: helsinki, block: true}
	cf, cloudflare := countingServer(t)
	h := newHarness(t, func(c *Config) { c.speedURL, c.ookla = cf.URL, f.factory })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	_, err := h.svc.SpeedTest(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v to stop", d)
	}
	if cloudflare.Load() != 0 || !f.closed.Load() {
		t.Errorf("Cloudflare asked %d times, closed %v", cloudflare.Load(), f.closed.Load())
	}
	if !h.svc.speedMu.TryLock() {
		t.Fatal("the speed test still holds the lock")
	}
	h.svc.speedMu.Unlock()
}

// While connected, speedtest.net is reached through the core's SOCKS
// inbound, with the credentials of this start.
func TestSpeedTestOoklaThroughCore(t *testing.T) {
	shortSpeedTest(t)
	f := &fakeOokla{server: helsinki, downRate: 1 << 20, upRate: 1 << 19, downMoved: 1 << 20, upMoved: 1 << 19}
	h := newHarness(t, func(c *Config) { c.ookla = f.factory })
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.SpeedTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.VPN || res.Server == "" || res.TestServer != "Elisa" {
		t.Errorf("result = %+v", res)
	}
	p, want := f.proxy.Load(), h.svc.proxyURL()
	if p == nil || p.Scheme != "socks5" || p.Host != h.listen.String() || p.User.Username() == "" || p.User.String() != want.User.String() {
		t.Errorf("proxy = %v, want the core's inbound with its credentials", p.Redacted())
	}
}

// The library takes http.DefaultClient for itself when loaded; the service
// gives it back, and its own tests never touch it.
func TestOoklaLeavesDefaultClient(t *testing.T) {
	if http.DefaultClient.Transport != nil {
		t.Fatalf("http.DefaultClient.Transport = %T", http.DefaultClient.Transport)
	}
	o := newOoklaTest(&url.URL{Scheme: "socks5", Host: "127.0.0.1:1", User: url.UserPassword("u", "p")}).(*ooklaLive)
	defer o.Close()
	if http.DefaultClient.Transport != nil {
		t.Errorf("http.DefaultClient.Transport = %T after a test was made", http.DefaultClient.Transport)
	}
	if p, _ := o.tr.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "www.speedtest.net"}}); p == nil || p.User.Username() != "u" {
		t.Errorf("the Ookla test goes through %v", p.Redacted())
	}
}

// Cloudflare's idea of where the test comes from picks the servers; a
// wrong or missing answer leaves speedtest.net's own.
func TestEgressLocation(t *testing.T) {
	answer := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			http.Error(w, "{}", http.StatusForbidden)
			return
		}
		w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	for _, c := range []struct {
		answer   string
		lat, lon float64
		ok       bool
	}{
		{answer: `{"country":"FI","city":"Helsinki","latitude":"60.16952","longitude":"24.93545"}`, lat: 60.16952, lon: 24.93545, ok: true},
		{answer: `{"country":"FI"}`},
		{answer: `{"latitude":"0","longitude":"0"}`},
		{answer: `{"latitude":"95","longitude":"10"}`},
		{answer: `<html>`},
	} {
		answer = c.answer
		loc := egressLocation(context.Background(), srv.Client(), srv.URL)
		if (loc != nil) != c.ok || (loc != nil && (loc.Lat != c.lat || loc.Lon != c.lon || loc.CC != "FI")) {
			t.Errorf("%s: got %+v", c.answer, loc)
		}
	}
	if loc := egressLocation(context.Background(), srv.Client(), "http://127.0.0.1:1/meta"); loc != nil {
		t.Errorf("unreachable: got %+v", loc)
	}
}

// Once closed, the library's workers wait for its timer instead of asking
// again at once.
func TestOoklaManagerStops(t *testing.T) {
	m := &ooklaManager{stop: make(chan struct{})}
	var calls atomic.Int32
	fn := m.wrap(func() { calls.Add(1) })
	fn()
	m.close()
	m.close()
	start := time.Now()
	fn()
	if calls.Load() != 1 || time.Since(start) < 40*time.Millisecond {
		t.Errorf("calls %d, waited %v", calls.Load(), time.Since(start))
	}
}

// A real speed test against speedtest.net from this machine, without the
// VPN: COSHIFT_LIVE_OOKLA=1 go test -run TestOoklaLive -v ./internal/service
func TestOoklaLive(t *testing.T) {
	if os.Getenv("COSHIFT_LIVE_OOKLA") == "" {
		t.Skip("set COSHIFT_LIVE_OOKLA=1 to measure against speedtest.net")
	}
	h := newHarness(t, func(c *Config) { c.ookla = nil })
	start := time.Now()
	res, err := h.svc.SpeedTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var peakDown, peakUp int64
	for _, e := range speedEvents(h) {
		peakDown, peakUp = max(peakDown, e.DownRate), max(peakUp, e.UpRate)
	}
	mbit := func(bps int64) float64 { return float64(bps) * 8 / 1e6 }
	t.Logf("server: %s, %s, %s; latency %d ms; download %.1f Mbit/s (live peak %.1f); upload %.1f Mbit/s (live peak %.1f); took %v",
		res.TestServer, res.TestCity, res.TestCountry, res.LatencyMS, mbit(res.DownloadBps), mbit(peakDown), mbit(res.UploadBps), mbit(peakUp), time.Since(start).Round(time.Millisecond))
	if res.TestServer == cloudflareName {
		t.Error("speedtest.net was not used")
	}
}
