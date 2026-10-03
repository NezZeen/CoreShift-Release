package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// SpeedResult is a speed test of the connection: through the VPN server
// while connected, else of the device's own.
type SpeedResult struct {
	// DownloadBps and UploadBps are bytes per second.
	DownloadBps int64 `json:"download_bps"`
	UploadBps   int64 `json:"upload_bps"`
	LatencyMS   int64 `json:"latency_ms"`
	VPN         bool  `json:"vpn"`
	// Server is the node tested, while connected.
	Server string `json:"server,omitempty"`
}

// ErrSpeedTestRunning means another speed test has not finished yet.
var ErrSpeedTestRunning = errors.New("a speed test is already running")

// speedServer answers /__down?bytes=N with N bytes and takes any body on
// /__up, from servers close to the user.
const speedServer = "https://speed.cloudflare.com"

const (
	speedDownStreams = 4
	// Cloudflare turns away a download of more than about 10 MB with
	// "429 Too Many Requests" for half an hour; smaller ones still go.
	speedDownChunk    = 8 << 20
	speedDownChunkMin = 1 << 20
	speedUpStreams    = 2
	speedUpChunk      = 8 << 20
	speedTick         = 250 * time.Millisecond
)

// How long each phase lasts; tests shorten them.
var (
	speedDownFor = 8 * time.Second
	speedUpFor   = 6 * time.Second
)

// SpeedTest measures the delay, then the download and upload speed, each
// over several connections at once, as speed test sites do. Progress
// arrives as "speedtest" events: Reason "latency", "download" and
// "upload" with the current rate, then "done" with the result or "error".
func (s *Service) SpeedTest(ctx context.Context) (SpeedResult, error) {
	if !s.speedMu.TryLock() {
		return SpeedResult{}, ErrSpeedTestRunning
	}
	defer s.speedMu.Unlock()
	res, err := s.speedTest(ctx)
	if err != nil {
		s.hub.publish(Event{Kind: "speedtest", Reason: "error", Error: err.Error()})
		return SpeedResult{}, err
	}
	s.hub.publish(Event{Kind: "speedtest", Reason: "done", DownRate: res.DownloadBps, UpRate: res.UploadBps, LatencyMS: res.LatencyMS})
	return res, nil
}

func (s *Service) speedTest(ctx context.Context) (SpeedResult, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	st := s.Status()
	res := SpeedResult{VPN: st.State == Connected}
	var proxy *url.URL
	if res.VPN {
		res.Server = st.Node
		proxy = s.proxyURL()
	}
	tr := newTransport(proxy)
	tr.MaxIdleConnsPerHost, tr.ForceAttemptHTTP2 = speedDownStreams, false
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	base := s.cfg.speedURL
	if base == "" {
		base = speedServer
	}

	lat, err := speedLatency(ctx, client, base)
	if err != nil {
		return res, fmt.Errorf("speed test server unreachable: %w", err)
	}
	res.LatencyMS = lat.Milliseconds()
	s.hub.publish(Event{Kind: "speedtest", Reason: "latency", LatencyMS: res.LatencyMS})

	res.DownloadBps, err = s.speedPhase(ctx, "download", speedDownStreams, speedDownFor, func(ctx context.Context, n *atomic.Int64) error {
		return speedDownload(ctx, client, base, n)
	})
	if err != nil {
		return res, err
	}
	res.UploadBps, err = s.speedPhase(ctx, "upload", speedUpStreams, speedUpFor, func(ctx context.Context, n *atomic.Int64) error {
		return speedUpload(ctx, client, base, n)
	})
	return res, err
}

// speedLatency is the quickest of a few empty requests over one kept-alive
// connection; the first, which also sets the connection up, does not count
// unless it is the only one that worked.
func speedLatency(ctx context.Context, client *http.Client, base string) (time.Duration, error) {
	var best, first time.Duration
	var err error
	for i := range 4 {
		start := time.Now()
		var resp *http.Response
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/__down?bytes=0", nil)
		if resp, err = client.Do(req); err != nil {
			if i == 0 {
				return 0, err
			}
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		d := max(time.Since(start), time.Millisecond)
		if resp.StatusCode != http.StatusOK {
			return 0, errors.New(resp.Status)
		}
		switch {
		case i == 0:
			first = d
		case best == 0 || d < best:
			best = d
		}
	}
	if best == 0 {
		best = first
	}
	return best, nil
}

// speedPhase runs streams copies of work for up to d, publishing the rate
// as it goes, and returns the rate over the whole phase. work adds the
// bytes it moves to its counter and repeats until its context ends. A
// stream that fails leaves the others to go on; the phase fails only when
// nothing went through.
func (s *Service) speedPhase(ctx context.Context, phase string, streams int, d time.Duration, work func(context.Context, *atomic.Int64) error) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	var moved atomic.Int64
	var wg sync.WaitGroup
	var firstErr atomic.Value
	start := time.Now()
	for range streams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := work(ctx, &moved); err != nil && ctx.Err() == nil {
				firstErr.CompareAndSwap(nil, err)
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	tick := time.NewTicker(speedTick)
	defer tick.Stop()
	last, lastAt := int64(0), start
	for {
		select {
		case <-done:
			total, elapsed := moved.Load(), time.Since(start)
			if total == 0 {
				if err, ok := firstErr.Load().(error); ok {
					return 0, fmt.Errorf("%s: %w", phase, err)
				}
				return 0, fmt.Errorf("%s: nothing went through", phase)
			}
			return int64(float64(total) / elapsed.Seconds()), nil
		case now := <-tick.C:
			n := moved.Load()
			rate := int64(float64(n-last) / now.Sub(lastAt).Seconds())
			last, lastAt = n, now
			e := Event{Kind: "speedtest", Reason: phase}
			if phase == "download" {
				e.DownRate = rate
			} else {
				e.UpRate = rate
			}
			s.hub.publish(e)
		}
	}
}

func speedDownload(ctx context.Context, client *http.Client, base string, moved *atomic.Int64) error {
	chunk := speedDownChunk
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/__down?bytes=%d", base, chunk), nil)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			// A server that limits the size takes smaller pieces.
			if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden) && chunk > speedDownChunkMin {
				chunk /= 2
				continue
			}
			return errors.New(resp.Status)
		}
		_, err = io.Copy(io.Discard, &countingReader{r: resp.Body, n: moved})
		resp.Body.Close()
		if err != nil && ctx.Err() == nil {
			return err
		}
	}
	return nil
}

func speedUpload(ctx context.Context, client *http.Client, base string, moved *atomic.Int64) error {
	for ctx.Err() == nil {
		body := &countingReader{r: io.LimitReader(zeroReader{}, speedUpChunk), n: moved}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/__up", body)
		req.ContentLength = speedUpChunk
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errors.New(resp.Status)
		}
	}
	return nil
}

// countingReader adds what passes through it to n.
type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	k, err := c.r.Read(p)
	c.n.Add(int64(k))
	return k, err
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
