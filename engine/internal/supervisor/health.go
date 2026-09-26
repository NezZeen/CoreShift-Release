package supervisor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

// checkHealth fetches Health.URL through the core's SOCKS inbound. The target
// host name is sent to the proxy unresolved, so this exercises the whole path
// to the server, not just the local port.
func checkHealth(ctx context.Context, socks netip.AddrPort, h Health) (time.Duration, error) {
	tr := &http.Transport{
		Proxy:             http.ProxyURL(&url.URL{Scheme: "socks5", Host: socks.String()}),
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
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
	if h.MaxLatency > 0 && lat > h.MaxLatency {
		return lat, fmt.Errorf("latency %s above %s", lat.Round(time.Millisecond), h.MaxLatency)
	}
	return lat, nil
}
