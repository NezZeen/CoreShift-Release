package supervisor

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

func getThrough(proxy *url.URL) error {
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	resp, err := c.Get("http://health.test/generate_204")
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

// While cores crash and are replaced, the SOCKS port never comes free:
// another program trying all along to listen on it never gets it, and
// after the swap the port reaches the new core.
func TestSwapNeverFreesThePort(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-after:700ms")
	t.Setenv("FAKECORE_SING_BOX", "crash-after:700ms")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	first, _ := h.s.CoreListen()

	var stop atomic.Bool
	taken := make(chan error, 1)
	tries := atomic.Int64{}
	go func() {
		for !stop.Load() {
			tries.Add(1)
			if ln, err := net.Listen("tcp", h.listen.String()); err == nil {
				ln.Close()
				taken <- nil
				return
			}
		}
		close(taken)
	}()
	h.waitFor(t, "swap to sing-box", 10*time.Second, isSwap(core.SingBox, ReasonExited))
	h.waitFor(t, "swap to mihomo", 10*time.Second, isSwap(core.Mihomo, ReasonExited))
	stop.Store(true)
	if _, ok := <-taken; ok {
		t.Fatal("another program took the SOCKS port during a swap")
	}
	if tries.Load() < 10 {
		t.Fatalf("only %d attempts to take the port", tries.Load())
	}
	last, ok := h.s.CoreListen()
	if !ok || last == first || last == h.listen || first == h.listen {
		t.Errorf("cores on %v then %v, the SOCKS port %v", first, last, h.listen)
	}
	if err := getThrough(h.s.SOCKSAuth().ProxyURL(h.listen)); err != nil {
		t.Fatalf("through the port after the swaps: %v", err)
	}

	h.s.Disconnect()
	ln, err := net.Listen("tcp", h.listen.String())
	if err != nil {
		t.Fatalf("the port is not free after Disconnect: %v", err)
	}
	ln.Close()
}

// The SOCKS port requires the credentials the TUN layer gets; the core
// behind it requires others, which no client is given.
func TestCorePortHasCredentialsOfItsOwn(t *testing.T) {
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	auth := h.s.SOCKSAuth()
	corePort, ok := h.s.CoreListen()
	if !ok {
		t.Fatal("no core")
	}
	if err := getThrough(auth.ProxyURL(h.listen)); err != nil {
		t.Fatalf("with the credentials: %v", err)
	}
	if err := getThrough(core.SOCKSAuth{}.ProxyURL(h.listen)); err == nil {
		t.Error("the port served a client without credentials")
	}
	for name, a := range map[string]core.SOCKSAuth{"none": {}, "the port's": auth} {
		if err := getThrough(a.ProxyURL(corePort)); err == nil {
			t.Errorf("the core's own port took %s credentials", name)
		}
	}
}

// Without the TUN layer the port is the open proxy of the user's
// programs; the core behind it still requires its credentials.
func TestOpenInbound(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.OpenInbound = true })
	connect(t, h, trojanLink)
	if err := getThrough(core.SOCKSAuth{}.ProxyURL(h.listen)); err != nil {
		t.Fatalf("without credentials: %v", err)
	}
	corePort, _ := h.s.CoreListen()
	if err := getThrough(core.SOCKSAuth{}.ProxyURL(corePort)); err == nil {
		t.Error("the core's own port is open")
	}
}

// A connection made while a crashed core is being replaced waits for the
// next one instead of failing.
func TestConnectionDuringSwapReachesTheNextCore(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-after:500ms")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	h.waitFor(t, "xray exits", 10*time.Second, func(e Event) bool {
		return e.Kind == EventCoreFailed && e.Core == core.Xray
	})
	// The next core is starting now.
	if err := getThrough(h.s.SOCKSAuth().ProxyURL(h.listen)); err != nil {
		t.Fatalf("during the swap: %v", err)
	}
	if st := h.s.Status(); st.Core != core.SingBox {
		t.Errorf("status %+v", st)
	}
}

// The traffic is counted at the SOCKS port: it grows with what passes,
// goes on across a swap, and starts from zero with the next connection.
func TestTrafficCountedAtThePort(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-after:1s")
	h := newHarness(t, nil)
	if _, _, err := h.s.Traffic(); err != ErrNotConnected {
		t.Fatalf("before Connect: %v", err)
	}
	connect(t, h, trojanLink)
	proxy := h.s.SOCKSAuth().ProxyURL(h.listen)
	if err := getThrough(proxy); err != nil {
		t.Fatal(err)
	}
	first, run, err := h.s.Traffic()
	if err != nil || first.Up <= 0 || first.Down <= 0 {
		t.Fatalf("after a request: %+v %v", first, err)
	}
	h.waitFor(t, "swap to sing-box", 10*time.Second, isSwap(core.SingBox, ReasonExited))
	if err := getThrough(proxy); err != nil {
		t.Fatal(err)
	}
	second, run2, _ := h.s.Traffic()
	if run2 != run || second.Up <= first.Up || second.Down <= first.Down {
		t.Errorf("after the swap: %+v (run %d), before %+v (run %d)", second, run2, first, run)
	}
	h.s.Disconnect()
	connect(t, h, trojanLink)
	third, run3, _ := h.s.Traffic()
	if run3 == run || third != (core.Traffic{}) {
		t.Errorf("the next connection: %+v (run %d)", third, run3)
	}
}

// Connect fails, and leaves the port alone, when another program holds it.
func TestConnectFailureFreesThePort(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-start")
	t.Setenv("FAKECORE_SING_BOX", "crash-start")
	t.Setenv("FAKECORE_MIHOMO", "crash-start")
	h := newHarness(t, nil)
	if err := h.s.Connect(context.Background(), mustNode(t, trojanLink), ""); err == nil {
		t.Fatal("connected with every core failing")
	}
	ln, err := net.Listen("tcp", h.listen.String())
	if err != nil {
		t.Fatalf("the port stays held after a failed Connect: %v", err)
	}
	ln.Close()
}
