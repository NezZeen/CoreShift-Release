package supervisor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"time"
)

// healthFallbacks are checked alongside Health.URL: services of different
// companies, so one being unreachable through a server, or blocked on the
// way, does not make a working connection look broken. Each answers 204 or
// 200 with a tiny body.
var healthFallbacks = []string{
	"http://cp.cloudflare.com/generate_204",
	"http://www.gstatic.com/generate_204",
	"http://captive.apple.com/hotspot-detect.html",
}

// healthURLs returns primary, then the fallbacks it is not already one of.
func healthURLs(primary string) []string {
	urls := []string{primary}
	for _, u := range healthFallbacks {
		if !slices.Contains(urls, u) {
			urls = append(urls, u)
		}
	}
	return urls
}

// checkHealth fetches Health.URL and the fallbacks through the core's SOCKS
// inbound, all at once, and succeeds as soon as one answers. The target host
// name is sent to the proxy unresolved, so this exercises the whole path to
// the server, not just the local port. The latency is the first answer's;
// when none answers, the error is Health.URL's.
func checkHealth(ctx context.Context, socks netip.AddrPort, h Health) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	urls := healthURLs(h.URL)
	type result struct {
		i   int
		lat time.Duration
		err error
	}
	results := make(chan result, len(urls))
	for i, u := range urls {
		go func() {
			lat, err := fetchThrough(ctx, socks, u)
			results <- result{i, lat, err}
		}()
	}
	errs := make([]error, len(urls))
	for range urls {
		r := <-results
		if r.err != nil {
			errs[r.i] = r.err
			continue
		}
		if h.MaxLatency > 0 && r.lat > h.MaxLatency {
			return r.lat, fmt.Errorf("latency %s above %s", r.lat.Round(time.Millisecond), h.MaxLatency)
		}
		return r.lat, nil
	}
	return 0, errs[0]
}

// delayThrough measures the delay through the proxy as Happ and v2rayNG
// report it: a first request sets up the connection to the server (TCP,
// TLS, the protocol's handshake), and a second one over it is timed. A
// fresh connection would add several round trips of setup to each result,
// reading as a much slower server than a ping shows. When the second
// request fails, the first one's time is the result.
func delayThrough(ctx context.Context, socks netip.AddrPort, u string, timeout time.Duration) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tr := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "socks5", Host: socks.String()})}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	get := func() (time.Duration, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return 0, err
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		lat := time.Since(start)
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			return lat, fmt.Errorf("unexpected response %s", resp.Status)
		}
		return lat, nil
	}
	first, err := get()
	if err != nil {
		return 0, err
	}
	if second, err := get(); err == nil {
		return second, nil
	}
	return first, nil
}

// fetchThrough requests u through the SOCKS proxy at socks and times it.
func fetchThrough(ctx context.Context, socks netip.AddrPort, u string) (time.Duration, error) {
	tr := &http.Transport{
		Proxy:             http.ProxyURL(&url.URL{Scheme: "socks5", Host: socks.String()}),
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	lat := time.Since(start)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return lat, fmt.Errorf("unexpected response %s", resp.Status)
	}
	return lat, nil
}
