package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// speedServerFake answers like speed.cloudflare.com; block, if set, holds
// every download until it is closed.
func speedServerFake(t *testing.T, block chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__down":
			n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
			if block != nil && n > 0 {
				select {
				case <-block:
				case <-r.Context().Done():
					return
				}
			}
			w.Header().Set("Content-Length", strconv.Itoa(n))
			buf := make([]byte, 32<<10)
			for n > 0 {
				k := min(n, len(buf))
				if _, err := w.Write(buf[:k]); err != nil {
					return
				}
				n -= k
			}
		case "/__up":
			io.Copy(io.Discard, r.Body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func shortSpeedTest(t *testing.T) {
	down, up := speedDownFor, speedUpFor
	speedDownFor, speedUpFor = 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { speedDownFor, speedUpFor = down, up })
}

func TestSpeedTest(t *testing.T) {
	shortSpeedTest(t)
	srv := speedServerFake(t, nil)
	h := newHarness(t, func(c *Config) { c.speedURL = srv.URL })

	res, err := h.svc.SpeedTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.VPN || res.DownloadBps <= 0 || res.UploadBps <= 0 || res.LatencyMS <= 0 {
		t.Errorf("result = %+v", res)
	}
	phases := map[string]bool{}
	for len(h.events) > 0 {
		if e := <-h.events; e.Kind == "speedtest" {
			phases[e.Reason] = true
		}
	}
	for _, p := range []string{"latency", "download", "upload", "done"} {
		if !phases[p] {
			t.Errorf("no %q event; got %v", p, phases)
		}
	}
}

func TestSpeedTestOneAtATime(t *testing.T) {
	shortSpeedTest(t)
	block := make(chan struct{})
	srv := speedServerFake(t, block)
	h := newHarness(t, func(c *Config) { c.speedURL = srv.URL })

	done := make(chan error, 1)
	go func() {
		_, err := h.svc.SpeedTest(context.Background())
		done <- err
	}()
	// The first test holds the lock while its downloads wait.
	deadline := time.Now().Add(5 * time.Second)
	for h.svc.speedMu.TryLock() {
		h.svc.speedMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("the first test never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := h.svc.SpeedTest(context.Background()); !errors.Is(err, ErrSpeedTestRunning) {
		t.Errorf("second test: %v", err)
	}
	close(block)
	<-done
}

func TestSpeedTestUnreachable(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.speedURL = "http://127.0.0.1:1" })
	if _, err := h.svc.SpeedTest(context.Background()); err == nil {
		t.Fatal("no error from an unreachable server")
	}
}

// Cloudflare answers a large download with 429; the test goes on in
// smaller pieces.
func TestSpeedTestTakesSmallerPiecesWhenLimited(t *testing.T) {
	shortSpeedTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__down":
			n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
			if n > 2<<20 {
				http.Error(w, "", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(n))
			w.Write(make([]byte, n))
		case "/__up":
			io.Copy(io.Discard, r.Body)
		}
	}))
	t.Cleanup(srv.Close)
	h := newHarness(t, func(c *Config) { c.speedURL = srv.URL })

	res, err := h.svc.SpeedTest(context.Background())
	if err != nil {
		t.Fatalf("SpeedTest: %v", err)
	}
	if res.DownloadBps <= 0 {
		t.Errorf("download = %d, want a rate", res.DownloadBps)
	}
}
