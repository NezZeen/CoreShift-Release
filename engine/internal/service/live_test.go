package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/store"
)

// The live tests run the service with the real cores, from XRAY_BIN,
// SINGBOX_BIN and MIHOMO_BIN as the core package's live tests do, against
// proxy servers sing-box runs on 127.0.0.1, with the TUN layer off: what a
// user does from the app, end to end, short of the system's routes.
//
//	XRAY_BIN=… SINGBOX_BIN=… MIHOMO_BIN=… go test ./internal/service -run Live

// liveCores returns the real cores, skipping the test without all three.
func liveCores(t *testing.T) map[core.Kind]string {
	t.Helper()
	bins := map[core.Kind]string{core.Xray: os.Getenv("XRAY_BIN"), core.SingBox: os.Getenv("SINGBOX_BIN"), core.Mihomo: os.Getenv("MIHOMO_BIN")}
	for k, p := range bins {
		if p == "" {
			t.Skipf("%s binary not set (XRAY_BIN, SINGBOX_BIN, MIHOMO_BIN)", k)
		}
	}
	return bins
}

// liveServer is a sing-box process serving proxy inbounds on 127.0.0.1.
type liveServer struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (s *liveServer) stop() {
	s.cmd.Process.Kill()
	<-s.done
}

func freeTCPPort(t *testing.T) uint16 {
	t.Helper()
	return freePort(t).Port()
}

// startProxyServer runs sing-box with inbounds and waits for each port.
func startProxyServer(t *testing.T, bin string, inbounds []map[string]any) *liveServer {
	t.Helper()
	dir := t.TempDir()
	cfg, _ := json.Marshal(map[string]any{
		"log":       map[string]any{"level": "warn"},
		"inbounds":  inbounds,
		"outbounds": []any{map[string]any{"type": "direct"}},
	})
	path := filepath.Join(dir, "server.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-c", path, "-D", dir)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &liveServer{cmd: cmd, done: make(chan struct{})}
	go func() { cmd.Wait(); close(s.done) }()
	t.Cleanup(s.stop)
	for _, in := range inbounds {
		ap := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(in["listen_port"].(uint16)))
		deadline := time.Now().Add(10 * time.Second)
		for !portUp(ap) {
			if time.Now().After(deadline) {
				t.Fatalf("test server did not open %s", ap)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return s
}

// spareListen returns a free SOCKS port below the ephemeral range whose
// next port, the supervisor's spare one, is free too.
func spareListen(t *testing.T) netip.AddrPort {
	t.Helper()
	lo := netip.MustParseAddr("127.0.0.1")
	for p := uint16(27000); p < 32000; p += 2 {
		a, b := netip.AddrPortFrom(lo, p), netip.AddrPortFrom(lo, p+1)
		if !portUp(a) && !portUp(b) {
			return a
		}
	}
	t.Fatal("no free port pair")
	return netip.AddrPort{}
}

func portUp(ap netip.AddrPort) bool {
	c, err := net.DialTimeout("tcp", ap.String(), 200*time.Millisecond)
	if err == nil {
		c.Close()
	}
	return err == nil
}

func ssInbound(port uint16) map[string]any {
	return map[string]any{"type": "shadowsocks", "listen": "127.0.0.1", "listen_port": port, "method": "aes-128-gcm", "password": "live-test"}
}

// ssLink names the server "localhost": panels' placeholders have loopback
// addresses, which a fetched subscription leaves out.
func ssLink(port uint16, name string) string {
	return fmt.Sprintf("ss://%s@localhost:%d#%s",base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:live-test")), port, url.PathEscape(name))
}

// killListener ends the process listening on port of 127.0.0.1, as a core
// that crashes: only the test's own core has that port.
func killListener(t *testing.T, port uint16) {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Stop-Process -Force -Id (Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort %d -State Listen).OwningProcess", port))
	} else {
		cmd = exec.Command("fuser", "-k", fmt.Sprintf("%d/tcp", port))
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("kill the core on port %d: %v %s", port, err, out)
	}
}

// liveHarness is the service with the real cores and a store, behind the
// API, with the TUN layer off.
type liveHarness struct {
	*harness
	st  *store.Store
	api *httptest.Server
	web *httptest.Server // health checks and pages fetched through the cores

	mu     sync.Mutex
	events []Event
}

func newLiveHarness(t *testing.T, edit func(*store.Settings)) *liveHarness {
	t.Helper()
	bins := liveCores(t)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generate_204" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write(make([]byte, 64<<10))
	}))
	t.Cleanup(web.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set := st.Settings()
	set.TUN = false
	set.Cores.HealthURL = web.URL + "/generate_204"
	set.Cores.HealthIntervalS, set.Cores.HealthFailures = 5, 2
	set.Cores.ReturnAfterMin = 0
	if edit != nil {
		edit(&set)
	}
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	speed := speedServerFake(t, nil)
	h := newHarness(t, func(c *Config) {
		c.Store = st
		c.Binaries = bins
		// Out of the ephemeral range, as 17890 is: the cores' counters
		// take ephemeral ports, and one could be the spare port next to
		// a SOCKS port picked from that range.
		c.Listen = spareListen(t)
		c.speedURL = speed.URL
		// Pings are real for the test servers; the well-known hosts of
		// checkReach answer, as on a computer with internet.
		c.physical = func() (ping.Bind, error) { return ping.Bind{}, nil }
		c.lookup = func(ctx context.Context, host string, server netip.AddrPort) (netip.Addr, error) {
			if host == "localhost" {
				return netip.MustParseAddr("127.0.0.1"), nil
			}
			return lookupHost(ctx, host, server)
		}
		c.tcpPing = func(ctx context.Context, ap netip.AddrPort, b ping.Bind) (time.Duration, error) {
			if !ap.Addr().IsLoopback() {
				return 10 * time.Millisecond, nil
			}
			return ping.TCP(ctx, ap, b, 1, time.Second)
		}
	})
	api := httptest.NewUnstartedServer(nil)
	api.Config.Handler = NewAPI(h.svc, token, netip.MustParseAddrPort(api.Listener.Addr().String()))
	api.Start()
	t.Cleanup(api.Close)
	h.listen = h.svc.cfg.Listen
	l := &liveHarness{harness: h, st: st, api: api, web: web}
	events, unsubscribe := h.svc.Subscribe(false)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range events {
			l.mu.Lock()
			l.events = append(l.events, e)
			l.mu.Unlock()
		}
	}()
	t.Cleanup(func() { h.svc.Disconnect(); unsubscribe() })
	return l
}

// fetch gets a page of the test web server through the running core's
// SOCKS port, as a program set up with the proxy does.
func (l *liveHarness) fetch(t *testing.T) error {
	t.Helper()
	c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(l.svc.proxyURL())}}
	resp, err := c.Get(l.web.URL + "/page")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, resp.Body)
	if err == nil && n != 64<<10 {
		err = fmt.Errorf("got %d bytes", n)
	}
	return err
}

func (l *liveHarness) seen(kind string, ok func(Event) bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.events, func(e Event) bool { return e.Kind == kind && (ok == nil || ok(e)) })
}

func (l *liveHarness) waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: never happened; status %+v", what, l.svc.Status())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (l *liveHarness) call(t *testing.T, method, path string, body, out any) int {
	t.Helper()
	return callJSON(t, l.api, method, path, body, out)
}

// TestLiveConnectSwapReturn: a subscription from a panel over HTTP,
// connecting from the app, a page through the core, the traffic counters,
// a core that crashes (the next takes over), the return to the primary,
// the manual core, latency, the speed test and disconnecting.
func TestLiveConnectSwapReturn(t *testing.T) {
	l := newLiveHarness(t, nil)
	bins := liveCores(t)
	pA, pB := freeTCPPort(t), freeTCPPort(t)
	startProxyServer(t, bins[core.SingBox], []map[string]any{ssInbound(pA), ssInbound(pB)})

	list := ssLink(pA, "Первый") + "\n" + ssLink(pB, "Второй") + "\n"
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=1000; download=2000; total=1000000000; expire=4102444800")
		w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("Живой тест")))
		io.WriteString(w, base64.StdEncoding.EncodeToString([]byte(list)))
	}))
	defer panel.Close()

	var sub subscriptionView
	if code := l.call(t, "POST", "/v1/subscriptions", map[string]string{"url": panel.URL + "/sub/abcdef0123456789"}, &sub); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	if len(sub.Nodes) != 2 || sub.DisplayName != "Живой тест" || sub.Info.Total != 1000000000 || sub.Info.Expire.IsZero() {
		t.Fatalf("subscription = %+v", sub)
	}
	for _, n := range sub.Nodes {
		if !slices.Equal(n.Cores, []core.Kind{core.Xray, core.SingBox, core.Mihomo}) {
			t.Errorf("%s: cores %v", n.Name, n.Cores)
		}
	}
	if code := l.call(t, "PUT", "/v1/selection", map[string]string{"subscription": sub.ID, "fingerprint": sub.Nodes[0].Fingerprint, "name": sub.Nodes[0].Name}, nil); code != http.StatusOK {
		t.Fatalf("select: %d", code)
	}

	// Latency, the light way: a TCP handshake with each server.
	var lat []NodeLatency
	if code := l.call(t, "POST", "/v1/latency", map[string]string{"subscription": sub.ID}, &lat); code != http.StatusOK {
		t.Fatalf("latency: %d", code)
	}
	for _, r := range lat {
		if r.Error != "" || r.LatencyMS <= 0 || r.Method != methodTCP {
			t.Errorf("latency %+v", r)
		}
	}

	var st Status
	if code := l.call(t, "POST", "/v1/connect", map[string]any{}, &st); code != http.StatusOK || st.State != Connected || st.Core != core.Xray {
		t.Fatalf("connect: %d %+v", code, st)
	}
	if err := l.fetch(t); err != nil {
		t.Fatalf("through xray: %v", err)
	}
	l.waitFor(t, "traffic counted", 10*time.Second, func() bool {
		return l.seen(kindTraffic, func(e Event) bool { return e.Down >= 64<<10 })
	})
	l.waitFor(t, "a health check passes", 15*time.Second, func() bool {
		return l.seen("health", func(e Event) bool { return e.Error == "" && !e.Probe })
	})

	// The core crashes: the next one takes the port over.
	killListener(t, l.listen.Port())
	l.waitFor(t, "swap to sing-box", 20*time.Second, func() bool {
		return l.seen("swap", func(e Event) bool { return e.From == string(core.Xray) && e.Core == string(core.SingBox) })
	})
	if s := l.svc.Status(); s.State != Connected || s.Core != core.SingBox || s.Failed[core.Xray] == "" {
		t.Errorf("after the swap: %+v", s)
	}
	if err := l.fetch(t); err != nil {
		t.Fatalf("through sing-box: %v", err)
	}

	// «Вернуть основное ядро».
	var reply map[string]any
	if code := l.call(t, "POST", "/v1/cores/return", nil, &reply); code != http.StatusOK {
		t.Fatalf("return: %d %v", code, reply)
	}
	l.waitFor(t, "back on xray", 20*time.Second, func() bool {
		s := l.svc.Status()
		return s.State == Connected && s.Core == core.Xray
	})
	if err := l.fetch(t); err != nil {
		t.Fatalf("back through xray: %v", err)
	}
	if code := l.call(t, "POST", "/v1/cores/return", nil, nil); code != http.StatusConflict {
		t.Errorf("return while on the primary: %d", code)
	}

	// The speed test goes through the core while connected.
	shortSpeedTest(t)
	var sp SpeedResult
	if code := l.call(t, "POST", "/v1/speedtest", nil, &sp); code != http.StatusOK || !sp.VPN || sp.DownloadBps <= 0 || sp.UploadBps <= 0 || sp.Server != "Первый" {
		t.Errorf("speed test: %d %+v", code, sp)
	}

	// Another server from the list while connected.
	if code := l.call(t, "POST", "/v1/connect", map[string]string{"subscription": sub.ID, "fingerprint": sub.Nodes[1].Fingerprint, "name": sub.Nodes[1].Name}, &st); code != http.StatusOK || st.Node != "Второй" {
		t.Fatalf("switch server: %d %+v", code, st)
	}
	if err := l.fetch(t); err != nil {
		t.Fatalf("through the second server: %v", err)
	}

	// A core chosen by hand, applied by reconnecting.
	set := l.st.Settings()
	set.Cores.Mode, set.Cores.Manual = store.ModeManual, core.Mihomo
	var saved store.Settings
	if code := l.call(t, "PUT", "/v1/settings", set, &saved); code != http.StatusOK {
		t.Fatalf("settings: %d", code)
	}
	if code := l.call(t, "GET", "/v1/status", nil, &st); code != http.StatusOK || !st.Pending {
		t.Errorf("changed settings must be pending: %+v", st)
	}
	st = Status{}
	if code := l.call(t, "POST", "/v1/reconnect", nil, &st); code != http.StatusOK || st.Core != core.Mihomo || st.Pending {
		t.Fatalf("reconnect: %d %+v", code, st)
	}
	if err := l.fetch(t); err != nil {
		t.Fatalf("through mihomo: %v", err)
	}

	if code := l.call(t, "POST", "/v1/disconnect", nil, &st); code != http.StatusOK || st.State != Idle {
		t.Fatalf("disconnect: %d %+v", code, st)
	}
	if portUp(l.listen) {
		t.Error("a core still listens after disconnecting")
	}
	var days Stats
	l.call(t, "GET", "/v1/stats?days=1", nil, &days)
	if b, _ := json.Marshal(days); !strings.Contains(string(b), `"down"`) {
		t.Errorf("stats = %s", b)
	}
}

// TestLiveServerFailover: the connected server goes down while the
// internet works; with «Менять сервер, если он не отвечает» the service
// moves to the next server of the subscription that answers.
func TestLiveServerFailover(t *testing.T) {
	l := newLiveHarness(t, nil)
	bins := liveCores(t)
	pA, pB := freeTCPPort(t), freeTCPPort(t)
	a := startProxyServer(t, bins[core.SingBox], []map[string]any{ssInbound(pA)})
	startProxyServer(t, bins[core.SingBox], []map[string]any{ssInbound(pB)})

	sub, err := l.st.Add(context.Background(), store.AddRequest{Name: "pasted", Content: ssLink(pA, "A") + "\n" + ssLink(pB, "B")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.st.Select(sub.ID, sub.Fingerprints()[0], "A"); err != nil {
		t.Fatal(err)
	}
	if err := l.svc.ConnectSelected(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := l.fetch(t); err != nil {
		t.Fatalf("through A: %v", err)
	}
	a.stop()
	l.waitFor(t, "failover to B", 90*time.Second, func() bool {
		s := l.svc.Status()
		return s.State == Connected && s.Node == "B"
	})
	if !l.seen("server", func(e Event) bool { return e.Reason == string(ReachServerDown) && e.From == "A" }) {
		t.Error("no server-down verdict")
	}
	if !l.seen("failover", func(e Event) bool { return e.From == "A" && e.Line == "B" }) {
		t.Error("no failover event")
	}
	if sel, _, _ := l.st.Selected(); sel.Name != "B" {
		t.Errorf("selection = %+v", sel)
	}
	l.waitFor(t, "a page through B", 20*time.Second, func() bool { return l.fetch(t) == nil })
}
