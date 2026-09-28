package supervisor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
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
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/proc"
	"coreshift/engine/internal/subscription"
)

var fakeCore string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakecore")
	if err != nil {
		panic(err)
	}
	fakeCore = filepath.Join(dir, "fakecore"+exeSuffix())
	build := exec.Command("go", "build", "-o", fakeCore, "./testdata/fakecore")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		panic("building fakecore: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// installCores copies fakecore under each core's name.
func installCores(t *testing.T, kinds ...core.Kind) map[core.Kind]string {
	t.Helper()
	src, err := os.ReadFile(fakeCore)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bins := map[core.Kind]string{}
	for _, k := range kinds {
		p := filepath.Join(dir, string(k)+exeSuffix())
		if err := os.WriteFile(p, src, 0o755); err != nil {
			t.Fatal(err)
		}
		bins[k] = p
	}
	return bins
}

func freePort(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return netip.MustParseAddrPort(ln.Addr().String())
}

type harness struct {
	s      *Supervisor
	events chan Event
	listen netip.AddrPort
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	h := &harness{events: make(chan Event, 10000), listen: freePort(t)}
	cfg := Config{
		Binaries:    installCores(t, core.Xray, core.SingBox, core.Mihomo),
		WorkDir:     t.TempDir(),
		Listen:      h.listen,
		ProbeListen: freePort(t),
		Health: Health{
			URL: "http://health.test/generate_204", Interval: 100 * time.Millisecond,
			Timeout: time.Second, Failures: 2,
		},
		StartTimeout: 10 * time.Second,
		OnEvent:      func(e Event) { h.events <- e },
	}
	if mutate != nil {
		mutate(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.s = s
	t.Cleanup(s.Disconnect)
	return h
}

// waitFor returns the first event matching pred, failing after timeout.
func (h *harness) waitFor(t *testing.T, what string, timeout time.Duration, pred func(Event) bool) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case e := <-h.events:
			if pred(e) {
				return e
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s; status %+v", what, h.s.Status())
		}
	}
}

// drain returns the events received so far.
func (h *harness) drain() []Event {
	var out []Event
	for {
		select {
		case e := <-h.events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func isSwap(to core.Kind, reason Reason) func(Event) bool {
	return func(e Event) bool { return e.Kind == EventSwap && e.Core == to && e.Reason == reason }
}

func mustNode(t *testing.T, link string) node.Node {
	t.Helper()
	n, err := subscription.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var (
	trojanLink = "trojan://pw@203.0.113.5:443?sni=t.example.com#Trojan"                                // every core
	hy2Link    = "hy2://auth@203.0.113.10:443/?sni=h.example.com&obfs=salamander&obfs-password=ob#Hy2" // no xray
)

func connect(t *testing.T, h *harness, link string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.s.Connect(ctx, mustNode(t, link), ""); err != nil {
		t.Fatalf("Connect: %v", err)
	}
}

func TestConnectUsesFirstCoreAndDisconnects(t *testing.T) {
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	st := h.s.Status()
	if st.State != Connected || st.Core != core.Xray || !slices.Equal(st.Chain, []core.Kind{core.Xray, core.SingBox, core.Mihomo}) {
		t.Fatalf("status = %+v", st)
	}
	// The generated config must hold the credentials only for the owner.
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(h.s.cfg.WorkDir, "xray", "config.json"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("config permissions = %v, %v", fi.Mode(), err)
		}
	}
	h.s.Disconnect()
	if st := h.s.Status(); st.State != Idle {
		t.Errorf("after disconnect: %+v", st)
	}
	if proc.PortOpen(h.listen) {
		t.Error("core still listening after Disconnect")
	}
}

func TestSwapWhenCoreRejectsConfig(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-start")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	if st := h.s.Status(); st.Core != core.SingBox || !strings.Contains(st.Failed[core.Xray], "config rejected") {
		t.Fatalf("status = %+v", st)
	}
	e := h.waitFor(t, "swap to sing-box", time.Second, isSwap(core.SingBox, ReasonStartFailed))
	if e.From != core.Xray {
		t.Errorf("swap from %s, want xray", e.From)
	}
}

func TestSwapWhenCoreCrashes(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-after:700ms")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	h.waitFor(t, "swap after crash", 10*time.Second, isSwap(core.SingBox, ReasonExited))
	st := h.s.Status()
	if st.State != Connected || st.Core != core.SingBox || !strings.Contains(st.Failed[core.Xray], "simulated crash") {
		t.Fatalf("status = %+v", st)
	}
}

func TestSwapOnHealthFailure(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "unhealthy-after:500ms")
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	h.waitFor(t, "swap after failed health checks", 10*time.Second, isSwap(core.SingBox, ReasonHealth))
	st := h.s.Status()
	if st.Core != core.SingBox || !strings.Contains(st.Failed[core.Xray], "503") {
		t.Fatalf("status = %+v", st)
	}
}

// When no core gets through, the network is the likely culprit: the
// connection stays up on the first core instead of going down.
func TestStaysConnectedWhenNoCoreChecksOut(t *testing.T) {
	for _, k := range []string{"XRAY", "SING_BOX", "MIHOMO"} {
		t.Setenv("FAKECORE_"+k, "unhealthy")
	}
	h := newHarness(t, nil)
	connect(t, h, trojanLink)
	h.waitFor(t, "no better core", 10*time.Second, func(e Event) bool { return e.Kind == EventNoBetter && e.Core == core.Xray })
	if st := h.s.Status(); st.State != Connected || st.Core != core.Xray || len(st.Failed) != 0 {
		t.Fatalf("status = %+v", st)
	}
	for _, e := range h.drain() {
		if e.Kind == EventSwap || e.Kind == EventCoreFailed {
			t.Errorf("unexpected %s event for %s", e.Kind, e.Core)
		}
	}
}

func TestHealthCheckUsesFallbacks(t *testing.T) {
	urls := healthURLs("http://own.test/204")
	if urls[0] != "http://own.test/204" || len(urls) != 1+len(healthFallbacks) {
		t.Errorf("urls = %v", urls)
	}
	if urls := healthURLs(healthFallbacks[0]); len(urls) != len(healthFallbacks) {
		t.Errorf("a fallback given as the URL is listed twice: %v", urls)
	}
}

func TestSkipsIncompatibleCore(t *testing.T) {
	h := newHarness(t, nil)
	connect(t, h, hy2Link)
	st := h.s.Status()
	if st.Core != core.SingBox || !slices.Equal(st.Chain, []core.Kind{core.SingBox, core.Mihomo}) {
		t.Fatalf("status = %+v", st)
	}
	for _, e := range h.drain() {
		if e.Kind == EventSwap || e.Kind == EventCoreFailed {
			t.Errorf("unexpected %s event for %s", e.Kind, e.Core)
		}
	}
}

func TestAllCoresFail(t *testing.T) {
	for _, k := range []string{"XRAY", "SING_BOX", "MIHOMO"} {
		t.Setenv("FAKECORE_"+k, "crash-start")
	}
	h := newHarness(t, nil)
	err := h.s.Connect(context.Background(), mustNode(t, trojanLink), "")
	if !errors.Is(err, ErrChainExhausted) {
		t.Fatalf("err = %v, want ErrChainExhausted", err)
	}
	for _, k := range []string{"xray", "sing-box", "mihomo"} {
		if !strings.Contains(err.Error(), k+": ") {
			t.Errorf("error does not explain %s: %v", k, err)
		}
	}
	if st := h.s.Status(); st.State != Failed {
		t.Errorf("state = %s", st.State)
	}
}

func TestManualModeDoesNotSwap(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-after:500ms")
	h := newHarness(t, func(c *Config) { c.Mode, c.ManualCore = Manual, core.Xray })
	connect(t, h, trojanLink)
	h.waitFor(t, "failed state", 10*time.Second, func(e Event) bool { return e.Kind == EventState && e.State == Failed })
	for _, e := range h.drain() {
		if e.Kind == EventSwap {
			t.Errorf("manual mode swapped to %s", e.Core)
		}
	}

	var ue *core.UnsupportedError
	if err := h.s.Connect(context.Background(), mustNode(t, hy2Link), ""); !errors.As(err, &ue) {
		t.Errorf("manual xray with hysteria2: err = %v, want UnsupportedError", err)
	}
}

func TestReturnToPrimary(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "xray-crashed-once")
	t.Setenv("FAKECORE_XRAY", "crash-start-once:"+marker)
	h := newHarness(t, func(c *Config) { c.ReturnToPrimaryAfter = 500 * time.Millisecond })
	connect(t, h, trojanLink)
	if st := h.s.Status(); st.Core != core.SingBox {
		t.Fatalf("expected to start on the backup, status = %+v", st)
	}
	e := h.waitFor(t, "return to xray", 15*time.Second, isSwap(core.Xray, ReasonReturn))
	if e.From != core.SingBox {
		t.Errorf("returned from %s", e.From)
	}
	if st := h.s.Status(); st.Core != core.Xray || st.State != Connected || len(st.Failed) != 0 {
		t.Errorf("status after return = %+v", st)
	}
}

func TestNoInstalledCoreSupportsNode(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Binaries = installCores(t, core.Xray) })
	err := h.s.Connect(context.Background(), mustNode(t, hy2Link), "")
	if !errors.Is(err, ErrNoCore) || !strings.Contains(err.Error(), "protocol:hysteria2") {
		t.Fatalf("err = %v", err)
	}
}

func TestBusyPortIsReported(t *testing.T) {
	h := newHarness(t, nil)
	ln, err := net.Listen("tcp", h.listen.String())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			io.Copy(io.Discard, c)
		}
	}()
	err = h.s.Connect(context.Background(), mustNode(t, trojanLink), "")
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("err = %v", err)
	}
}

func TestRelativeBinaryPath(t *testing.T) {
	bin := installCores(t, core.Xray)[core.Xray]
	t.Chdir(filepath.Dir(bin))
	rel := "." + string(filepath.Separator) + filepath.Base(bin)
	h := newHarness(t, func(c *Config) { c.Binaries = map[core.Kind]string{core.Xray: rel} })
	connect(t, h, trojanLink)
	if st := h.s.Status(); st.Core != core.Xray {
		t.Fatalf("status = %+v", st)
	}
}

func TestLatencyTest(t *testing.T) {
	t.Setenv("FAKECORE_MIHOMO", "unhealthy")
	h := newHarness(t, func(c *Config) { c.Priority = []core.Kind{core.Xray, core.SingBox, core.Mihomo} })
	nodes := []node.Node{mustNode(t, trojanLink), mustNode(t, hy2Link), mustNode(t, trojanLink)}
	h.s.SetPolicy(Policy{Priority: []core.Kind{core.Xray, core.SingBox, core.Mihomo}, Health: h.s.cfg.Health})
	var mu sync.Mutex
	got := map[int]LatencyResult{}
	h.s.TestLatency(context.Background(), nodes, nil, 2, func(r LatencyResult) {
		mu.Lock()
		got[r.Index] = r
		mu.Unlock()
	})
	if len(got) != 3 {
		t.Fatalf("results: %+v", got)
	}
	if r := got[0]; r.Err != nil || r.Core != core.Xray || r.Latency <= 0 {
		t.Errorf("trojan: %+v", r)
	}
	if r := got[1]; r.Err != nil || r.Core != core.SingBox {
		t.Errorf("hysteria2 must be tested with sing-box: %+v", r)
	}

	// A core that fails the test gives way to the node's next core.
	h.s.SetPolicy(Policy{Priority: []core.Kind{core.Mihomo, core.Xray, core.SingBox}, Health: h.s.cfg.Health})
	h.s.TestLatency(context.Background(), nodes[:1], nil, 1, func(r LatencyResult) { got[98] = r })
	if r := got[98]; r.Err != nil || r.Core != core.Xray || r.Latency <= 0 {
		t.Errorf("fallback after unhealthy mihomo: %+v", r)
	}

	// Manual mode tests with the chosen core; an unhealthy one reports why.
	h.s.SetPolicy(Policy{Mode: Manual, ManualCore: core.Mihomo, Health: h.s.cfg.Health})
	h.s.TestLatency(context.Background(), nodes[:1], nil, 1, func(r LatencyResult) { got[99] = r })
	if r := got[99]; r.Err == nil || r.Core != core.Mihomo || !strings.Contains(r.Err.Error(), "503") {
		t.Errorf("unhealthy core: %+v", r)
	}
	if entries, _ := os.ReadDir(filepath.Join(h.s.cfg.WorkDir, "latency")); len(entries) != 0 {
		t.Errorf("test configs left behind: %d", len(entries))
	}
}

func TestReturnToPrimaryOnRequest(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "xray-crashed-once")
	t.Setenv("FAKECORE_XRAY", "crash-start-once:"+marker)
	// No automatic return: only the request moves back.
	h := newHarness(t, nil)
	if err := h.s.ReturnToPrimary(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Errorf("idle: err = %v, want ErrNotConnected", err)
	}
	connect(t, h, trojanLink)
	if st := h.s.Status(); st.Core != core.SingBox {
		t.Fatalf("expected to start on the backup, status = %+v", st)
	}
	if err := h.s.ReturnToPrimary(context.Background()); err != nil {
		t.Fatalf("ReturnToPrimary: %v", err)
	}
	h.waitFor(t, "return to xray", 10*time.Second, isSwap(core.Xray, ReasonReturn))
	h.waitFor(t, "xray connected", 10*time.Second, func(e Event) bool { return e.Kind == EventState && e.State == Connected && e.Core == core.Xray })
	if err := h.s.ReturnToPrimary(context.Background()); !errors.Is(err, ErrOnPrimary) {
		t.Errorf("on the primary: err = %v, want ErrOnPrimary", err)
	}
}
