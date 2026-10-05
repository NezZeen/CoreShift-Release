package supervisor

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

// slowSOCKS is a SOCKS5 proxy that takes setup to set each connection up,
// as a proxy's handshake with a distant server does, and counts them.
func slowSOCKS(t *testing.T, setup time.Duration) (netip.AddrPort, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var conns atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go serveSOCKS(c, setup)
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String()), &conns
}

func serveSOCKS(c net.Conn, setup time.Duration) {
	defer c.Close()
	buf := make([]byte, 262)
	// Greeting: version, methods; no authentication.
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return
	}
	if _, err := io.ReadFull(c, buf[:buf[1]]); err != nil {
		return
	}
	c.Write([]byte{5, 0})
	// CONNECT request.
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	var host string
	switch buf[3] {
	case 1:
		io.ReadFull(c, buf[:4])
		host = net.IP(buf[:4]).String()
	case 3:
		io.ReadFull(c, buf[:1])
		n := int(buf[0])
		io.ReadFull(c, buf[:n])
		host = string(buf[:n])
	default:
		return
	}
	io.ReadFull(c, buf[:2])
	port := binary.BigEndian.Uint16(buf[:2])
	time.Sleep(setup)
	up, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	go io.Copy(up, c)
	io.Copy(c, up)
}

// healthServers stands in for Health.URL and the fallbacks: each answers
// with code after delay, and counts the requests.
func healthServers(t *testing.T, primary, fallback http.HandlerFunc) (string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var first, rest atomic.Int32
	count := func(n *atomic.Int32, h http.HandlerFunc) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			h(w, r)
		}))
		t.Cleanup(srv.Close)
		// First (cleanups run last in, first out): ends a request left hanging.
		t.Cleanup(srv.CloseClientConnections)
		return srv
	}
	p := count(&first, primary)
	f1, f2 := count(&rest, fallback), count(&rest, fallback)
	old := healthFallbacks
	healthFallbacks = []string{f1.URL + "/generate_204", f2.URL + "/generate_204"}
	t.Cleanup(func() { healthFallbacks = old })
	return p.URL + "/generate_204", &first, &rest
}

func answer(code int, after time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(after):
		case <-r.Context().Done():
		}
		w.WriteHeader(code)
	}
}

// A connection that works costs one request per check: the fallbacks are
// for when the first address does not answer.
func TestHealthCheckAsksFallbacksOnlyWhenNeeded(t *testing.T) {
	socks, _ := slowSOCKS(t, 0)
	proxy := core.SOCKSAuth{}.ProxyURL(socks)
	h := Health{Timeout: 5 * time.Second}

	t.Run("primary answers", func(t *testing.T) {
		u, first, rest := healthServers(t, answer(http.StatusNoContent, 0), answer(http.StatusNoContent, 0))
		h.URL = u
		for range 3 {
			if _, err := checkHealth(context.Background(), proxy, h); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(healthFallbackAfter + 200*time.Millisecond)
		if first.Load() != 3 || rest.Load() != 0 {
			t.Errorf("requests: %d to the first address, %d to the fallbacks; want 3 and 0", first.Load(), rest.Load())
		}
	})

	t.Run("primary fails", func(t *testing.T) {
		u, _, rest := healthServers(t, answer(http.StatusBadGateway, 0), answer(http.StatusNoContent, 0))
		h.URL = u
		start := time.Now()
		if _, err := checkHealth(context.Background(), proxy, h); err != nil {
			t.Fatal(err)
		}
		// Asked at once, not after healthFallbackAfter.
		if d := time.Since(start); d >= healthFallbackAfter {
			t.Errorf("the fallbacks were asked only after %v", d)
		}
		if rest.Load() == 0 {
			t.Error("no fallback was asked")
		}
	})

	t.Run("primary hangs", func(t *testing.T) {
		u, _, _ := healthServers(t, answer(http.StatusNoContent, time.Minute), answer(http.StatusNoContent, 0))
		h.URL = u
		start := time.Now()
		lat, err := checkHealth(context.Background(), proxy, h)
		if err != nil {
			t.Fatal(err)
		}
		d := time.Since(start)
		if d < healthFallbackAfter || d > healthFallbackAfter+2*time.Second {
			t.Errorf("answered after %v; want soon after %v", d, healthFallbackAfter)
		}
		// The latency is the fallback's own.
		if lat >= healthFallbackAfter {
			t.Errorf("latency %v counts the wait for the first address", lat)
		}
	})

	t.Run("none answers", func(t *testing.T) {
		u, _, _ := healthServers(t, answer(http.StatusBadGateway, 0), answer(http.StatusServiceUnavailable, 0))
		h.URL = u
		_, err := checkHealth(context.Background(), proxy, h)
		if err == nil || !strings.Contains(err.Error(), "502") || strings.Count(err.Error(), "503") != 2 {
			t.Errorf("err = %v; want every address named with its answer", err)
		}
	})
}

func TestDelayLeavesOutConnectionSetup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	socks, conns := slowSOCKS(t, 300*time.Millisecond)

	lat, err := delayThrough(context.Background(), core.SOCKSAuth{}.ProxyURL(socks), srv.URL+"/generate_204", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// The second request reuses the connection the first one set up.
	if lat >= 200*time.Millisecond {
		t.Errorf("delay %v includes the connection's setup", lat)
	}
	if n := conns.Load(); n != 1 {
		t.Errorf("%d connections through the proxy, want 1", n)
	}

	// A fresh connection per request, as the health check makes, pays the
	// setup each time.
	full, err := fetchThrough(context.Background(), core.SOCKSAuth{}.ProxyURL(socks), srv.URL+"/generate_204")
	if err != nil || full < 300*time.Millisecond {
		t.Errorf("health check: %v, %v", full, err)
	}
}

// A check cut short by a disconnect or a switch of servers is not reported:
// the journal said "the check failed: context canceled" on every switch.
func TestCancelledCheckIsNotReported(t *testing.T) {
	socks, _ := slowSOCKS(t, 5*time.Second)
	var events []Event
	s := &Supervisor{cfg: Config{Health: Health{URL: "http://health.test/generate_204", Timeout: 5 * time.Second},
		OnEvent: func(e Event) { events = append(events, e) }}.withDefaults()}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	if _, err := s.check(ctx, &process{kind: core.Xray, listen: socks}); err == nil {
		t.Fatal("a cancelled check passed")
	}
	if len(events) != 0 {
		t.Errorf("a cancelled check was reported: %+v", events)
	}

	// One that runs its course is.
	if _, err := s.check(context.Background(), &process{kind: core.Xray, listen: netip.MustParseAddrPort("127.0.0.1:1")}); err == nil {
		t.Fatal("a check through a closed port passed")
	}
	if len(events) != 1 || events[0].Kind != EventHealth || events[0].Err == nil {
		t.Errorf("events = %+v, want one failed check", events)
	}
}
