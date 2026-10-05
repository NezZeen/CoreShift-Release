package service

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
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
	"coreshift/engine/internal/dnsguard"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/proc"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/subscription"
	"coreshift/engine/internal/supervisor"
	"coreshift/engine/internal/tunlayer"
)

var fakeCore string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakecore")
	if err != nil {
		panic(err)
	}
	fakeCore = filepath.Join(dir, "fakecore"+exe())
	build := exec.Command("go", "build", "-o", fakeCore, "../supervisor/testdata/fakecore")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		panic("building fakecore: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func exe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func installCores(t *testing.T) map[core.Kind]string {
	t.Helper()
	src, err := os.ReadFile(fakeCore)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bins := map[core.Kind]string{}
	for _, k := range []core.Kind{core.Xray, core.SingBox, core.Mihomo} {
		p := filepath.Join(dir, string(k)+exe())
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

// callLog records the order in which the fakes are used.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	l.calls = append(l.calls, s)
	l.mu.Unlock()
}

func (l *callLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

// before reports whether a occurs, and occurs before b, after position from.
func (l *callLog) before(from int, a, b string) bool {
	calls := l.get()[from:]
	ia, ib := slices.Index(calls, a), slices.Index(calls, b)
	return ia >= 0 && (ib < 0 || ia < ib)
}

type fakeGuard struct {
	log      *callLog
	applyErr error
	onApply  func() // runs as the guard applies
	mu       sync.Mutex
	applied  *dnsguard.Config
}

func (g *fakeGuard) Apply(_ context.Context, cfg dnsguard.Config) error {
	g.log.add("dns.apply")
	if g.onApply != nil {
		g.onApply()
	}
	if g.applyErr != nil {
		return g.applyErr
	}
	g.mu.Lock()
	g.applied = &cfg
	g.mu.Unlock()
	return nil
}

func (g *fakeGuard) Revert(context.Context) error {
	g.log.add("dns.revert")
	g.mu.Lock()
	g.applied = nil
	g.mu.Unlock()
	return nil
}

func (g *fakeGuard) Recover(context.Context) error { g.log.add("dns.recover"); return nil }

func (g *fakeGuard) active() *dnsguard.Config {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.applied
}

type fakeTUN struct {
	log      *callLog
	startErr error
	hang     bool // Start waits until cancelled
	mu       sync.Mutex
	opts     tunlayer.Options
	inst     *fakeInstance
}

func (f *fakeTUN) Start(ctx context.Context, o tunlayer.Options) (TUNInstance, error) {
	f.log.add("tun.start")
	if f.startErr != nil {
		return nil, f.startErr
	}
	if f.hang {
		// Like an adapter Windows has not freed yet: only cancelling ends it.
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opts = o
	f.inst = &fakeInstance{log: f.log, exited: make(chan struct{})}
	return f.inst, nil
}

func (f *fakeTUN) instance() *fakeInstance {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inst
}

type fakeInstance struct {
	log    *callLog
	once   sync.Once
	exited chan struct{}
}

func (i *fakeInstance) Exited() <-chan struct{} { return i.exited }
func (i *fakeInstance) ExitError() error        { return errors.New("tun exited: wintun failed") }
func (i *fakeInstance) Stop()                   { i.log.add("tun.stop"); i.once.Do(func() { close(i.exited) }) }
func (i *fakeInstance) crash()                  { i.once.Do(func() { close(i.exited) }) }

type harness struct {
	mu      sync.Mutex
	lookups []netip.AddrPort // the resolver of each server lookup

	svc    *Service
	log    *callLog
	tun    *fakeTUN
	guard  *fakeGuard
	listen netip.AddrPort
	events <-chan Event
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	log := &callLog{}
	h := &harness{log: log, tun: &fakeTUN{log: log}, guard: &fakeGuard{log: log}, listen: freePort(t)}
	cfg := Config{
		DataDir:  t.TempDir(),
		Binaries: installCores(t),
		Listen:   h.listen,
		Options: Options{
			Health: supervisor.Health{
				URL: "http://health.test/generate_204", Interval: 100 * time.Millisecond,
				Timeout: time.Second, Failures: 2,
			},
			TUN: true,
		},
		guard: h.guard,
		tun:   h.tun,
		resolvers: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("192.0.2.53")}, nil
		},
		lookup: func(_ context.Context, _ string, server netip.AddrPort) (netip.Addr, error) {
			h.mu.Lock()
			h.lookups = append(h.lookups, server)
			h.mu.Unlock()
			return netip.MustParseAddr("198.51.100.7"), nil
		},
		// No real pings from tests.
		physical: func() (ping.Bind, error) { return ping.Bind{}, errors.New("no network in tests") },
		icmpPing: func(context.Context, netip.Addr, ping.Bind) (time.Duration, error) {
			return 0, errors.New("unreachable")
		},
		tcpPing: func(context.Context, netip.AddrPort, ping.Bind) (time.Duration, error) {
			return 0, errors.New("unreachable")
		},
		// No speedtest.net from tests: the speed test goes to speedURL.
		ookla: noOokla,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	var unsubscribe func()
	h.events, unsubscribe = svc.Subscribe(false)
	t.Cleanup(func() {
		svc.Disconnect()
		unsubscribe()
	})
	return h
}

func (h *harness) connect(t *testing.T, link string) error {
	t.Helper()
	n, err := subscription.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return h.svc.Connect(ctx, n)
}

func (h *harness) waitState(t *testing.T, want State, timeout time.Duration) Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st := h.svc.Status(); st.State == want {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("state never became %s; status %+v, calls %v", want, h.svc.Status(), h.log.get())
	return Status{}
}

const trojanLink = "trojan://pw@203.0.113.5:443?sni=t.example.com#Trojan"

func TestConnectOrderAndDisconnect(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	st := h.svc.Status()
	if st.State != Connected || st.Core != core.Xray || !st.TUN {
		t.Fatalf("status = %+v", st)
	}
	if !h.log.before(0, "tun.start", "dns.apply") {
		t.Errorf("TUN must be up before DNS is redirected: %v", h.log.get())
	}

	o := h.tun.opts
	if o.Upstream != h.listen || o.DNS.Direct != "192.0.2.53" ||
		!slices.Equal(o.BypassAddresses, []netip.Prefix{netip.MustParsePrefix("203.0.113.5/32")}) {
		t.Errorf("tun options = %+v", o)
	}
	for _, bin := range h.svc.cfg.Binaries {
		if !filepath.IsAbs(bin) || !slices.Contains(o.BypassProcesses, bin) {
			t.Errorf("core %s must bypass the TUN by absolute path: %v", bin, o.BypassProcesses)
		}
	}
	g := h.guard.active()
	if g == nil || g.Interface != tunlayer.DefaultInterface || !slices.Equal(g.Servers, []netip.Addr{netip.MustParseAddr("172.19.0.2")}) {
		t.Errorf("dns guard = %+v", g)
	}

	mark := len(h.log.get())
	h.svc.Disconnect()
	if !h.log.before(mark, "dns.revert", "tun.stop") {
		t.Errorf("DNS must be restored before the TUN goes down: %v", h.log.get()[mark:])
	}
	if st := h.svc.Status(); st.State != Idle {
		t.Errorf("after disconnect: %+v", st)
	}
	if proc.PortOpen(h.listen) {
		t.Error("core still running after disconnect")
	}
}

func TestDNSFailureRollsBack(t *testing.T) {
	h := newHarness(t, nil)
	h.guard.applyErr = errors.New("access denied")
	err := h.connect(t, trojanLink)
	if err == nil || !strings.Contains(err.Error(), "redirect system DNS") {
		t.Fatalf("err = %v", err)
	}
	if !slices.Contains(h.log.get(), "tun.stop") || proc.PortOpen(h.listen) {
		t.Errorf("TUN and core must be stopped: %v", h.log.get())
	}
	if st := h.svc.Status(); st.State != Failed || !strings.Contains(st.Error, "access denied") {
		t.Errorf("status = %+v", st)
	}
}

func TestTUNFailureStopsCore(t *testing.T) {
	h := newHarness(t, nil)
	h.tun.startErr = errors.New("wintun: access denied")
	if err := h.connect(t, trojanLink); err == nil {
		t.Fatal("expected an error")
	}
	if slices.Contains(h.log.get(), "dns.apply") {
		t.Error("DNS must not be redirected without a TUN")
	}
	if proc.PortOpen(h.listen) {
		t.Error("core left running")
	}
}

func TestTUNCrashRestoresDNS(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	mark := len(h.log.get())
	h.tun.instance().crash()
	st := h.waitState(t, Failed, 5*time.Second)
	if !strings.Contains(st.Error, "TUN layer stopped") || !strings.Contains(st.Error, "wintun failed") {
		t.Errorf("error = %q", st.Error)
	}
	if !slices.Contains(h.log.get()[mark:], "dns.revert") || h.guard.active() != nil {
		t.Errorf("DNS not restored after the TUN died: %v", h.log.get()[mark:])
	}
	if proc.PortOpen(h.listen) {
		t.Error("core left running")
	}
}

// A TUN layer that dies while connecting, here as DNS is redirected into
// it, fails the connection: never "connected", and the DNS is given back.
func TestTUNDyingWhileConnectingIsNoSuccess(t *testing.T) {
	h := newHarness(t, nil)
	h.guard.onApply = func() { h.tun.instance().crash() }
	err := h.connect(t, trojanLink)
	if err == nil || !strings.Contains(err.Error(), "TUN layer stopped") {
		t.Fatalf("err = %v", err)
	}
	for {
		select {
		case e := <-h.events:
			if e.Kind == "state" && e.State == Connected {
				t.Fatal("reported connected")
			}
			continue
		default:
		}
		break
	}
	if st := h.svc.Status(); st.State != Failed {
		t.Errorf("status = %+v", st)
	}
	if h.guard.active() != nil || proc.PortOpen(h.listen) {
		t.Errorf("DNS or core left behind: %v", h.log.get())
	}
}

func TestAllCoresFailingRestoresDNS(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-after:700ms")
	t.Setenv("FAKECORE_SING_BOX", "crash-start")
	t.Setenv("FAKECORE_MIHOMO", "crash-start")
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	st := h.waitState(t, Failed, 10*time.Second)
	if !strings.Contains(st.Error, "every compatible core failed") {
		t.Errorf("error = %q", st.Error)
	}
	calls := h.log.get()
	if !slices.Contains(calls, "dns.revert") || !slices.Contains(calls, "tun.stop") || h.guard.active() != nil {
		t.Errorf("not torn down: %v", calls)
	}
}

func TestHostnameResolvedBeforeConnecting(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, "trojan://pw@vpn.example.com:443#Named"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.tun.opts.BypassAddresses, []netip.Prefix{netip.MustParsePrefix("198.51.100.7/32")}) {
		t.Errorf("bypass = %v", h.tun.opts.BypassAddresses)
	}
	cfg, err := os.ReadFile(filepath.Join(h.svc.cfg.DataDir, "work", "xray", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `"198.51.100.7"`) || !strings.Contains(string(cfg), `"vpn.example.com"`) {
		t.Errorf("core must dial the IP and keep the name as SNI:\n%s", cfg)
	}
}

func TestMissingSystemResolverFallsBack(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.resolvers = func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("no adapters") }
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if h.tun.opts.DNS.Direct != "1.1.1.1" {
		t.Errorf("direct DNS = %q", h.tun.opts.DNS.Direct)
	}
	for {
		select {
		case e := <-h.events:
			if e.Kind == "dns" && strings.Contains(e.Error, "no system resolver") {
				return
			}
		case <-time.After(time.Second):
			t.Fatal("no warning event about the missing resolver")
		}
	}
}

func TestSOCKSOnlyMode(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.TUN = false })
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if st := h.svc.Status(); st.State != Connected || st.TUN {
		t.Errorf("status = %+v", st)
	}
	if calls := h.log.get(); slices.Contains(calls, "tun.start") || slices.Contains(calls, "dns.apply") {
		t.Errorf("TUN or DNS touched in SOCKS mode: %v", calls)
	}
}

func TestReconnectReplacesConnection(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if err := h.connect(t, "trojan://pw@203.0.113.9:443?sni=t.example.com#Second"); err != nil {
		t.Fatal(err)
	}
	st := h.svc.Status()
	if st.State != Connected || st.Node != "Second" {
		t.Fatalf("status = %+v", st)
	}
	calls := h.log.get()
	if n := countOf(calls, "tun.start"); n != 2 || countOf(calls, "tun.stop") != 1 {
		t.Errorf("expected one full restart: %v", calls)
	}
}

func countOf(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}

func TestAdapterRaceIsRecognised(t *testing.T) {
	race := errors.New("tun exited (exit status 1): FATAL[0015] start service: start inbound/tun[tun-in]: configure tun interface: (create adapter: Cannot create a file when that file already exists. | open existing adapter: Element not found.)")
	if !isAdapterRace(race) {
		t.Error("the Wintun race was not recognised")
	}
	if !isAdapterRace(errors.New("tun exited (exit status 1): FATAL[0000] start service: start inbound/tun[tun-in]: configure tun interface: set ipv4 address: The object already exists.")) {
		t.Error("the address race was not recognised")
	}
	if isAdapterRace(errors.New("tun exited (exit status 1): FATAL decode config: unknown field")) {
		t.Error("a config error was taken for the race")
	}
}

func TestRussiaDirectDownloadsRuleSets(t *testing.T) {
	var mu sync.Mutex
	var via []string
	h := newHarness(t, func(c *Config) {
		c.DNS.RussiaDirect = true
		c.fetchRuleSet = func(_ context.Context, _ string, proxy *url.URL) ([]byte, error) {
			mu.Lock()
			if proxy == nil {
				via = append(via, "direct")
			} else {
				via = append(via, proxy.Host)
				// The core's inbound takes only its credentials.
				if p, ok := proxy.User.Password(); !ok || p == "" || proxy.User.Username() == "" {
					via[len(via)-1] += " without credentials"
				}
			}
			mu.Unlock()
			if proxy == nil {
				return nil, errors.New("blocked")
			}
			return []byte("SRS\x02fake"), nil
		}
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.tun.mu.Lock()
	o := h.tun.opts
	h.tun.mu.Unlock()
	if !slices.Contains(o.DNS.DirectSuffixes, "xn--p1ai") || !slices.Contains(o.DNS.DirectSuffixes, "2ip.io") || len(o.DNS.DirectRuleSets) != 1 || len(o.DNS.DirectIPRuleSets) != 1 ||
		len(o.DNS.ProxyRuleSets) != 2 || o.DNS.ProxyRuleSets[0].Tag != "geosite-category-media-ru-blocked" || o.DNS.ProxyRuleSets[1].Tag != "geosite-google" {
		t.Fatalf("dns options = %+v", o.DNS)
	}
	if _, err := os.Stat(o.DNS.DirectIPRuleSets[0].Path); err != nil {
		t.Errorf("rule set not on disk: %v", err)
	}
	if via[0] != h.listen.String() {
		t.Errorf("first download went via %s, want the core's SOCKS %s", via[0], h.listen)
	}

	// Already on disk: no more downloads.
	n := len(via)
	h.svc.Disconnect()
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if len(via) != n {
		t.Errorf("downloaded again: %v", via[n:])
	}
}

func TestRussiaDirectWithoutRuleSetsStillConnects(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.DNS.RussiaDirect = true
		c.fetchRuleSet = func(context.Context, string, *url.URL) ([]byte, error) {
			return []byte("<html>blocked</html>"), nil
		}
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.tun.mu.Lock()
	o := h.tun.opts
	h.tun.mu.Unlock()
	if !slices.Contains(o.DNS.DirectSuffixes, "ru") || len(o.DNS.DirectRuleSets)+len(o.DNS.DirectIPRuleSets)+len(o.DNS.ProxyRuleSets) != 0 {
		t.Errorf("dns options = %+v", o.DNS)
	}
	warned := false
	for len(h.events) > 0 {
		if e := <-h.events; e.Kind == "rules" && strings.Contains(e.Error, "not a rule set") {
			warned = true
		}
	}
	if !warned {
		t.Error("no warning about the missing rule sets")
	}
}

func TestIPv6Tunnel(t *testing.T) {
	for _, hostV6 := range []bool{false, true} {
		h := newHarness(t, func(c *Config) {
			c.IPv6 = true
			c.hostIPv6 = func() bool { return hostV6 }
			c.ipv6Off = func() bool { return false } // whatever the test machine has
		})
		if err := h.connect(t, trojanLink); err != nil {
			t.Fatal(err)
		}
		h.tun.mu.Lock()
		o := h.tun.opts
		h.tun.mu.Unlock()
		if o.Address6 != tunlayer.DefaultAddress6 || o.DNS.DirectIPv4Only == hostV6 {
			t.Errorf("host IPv6 %v: address6 %v, direct IPv4-only %v", hostV6, o.Address6, o.DNS.DirectIPv4Only)
		}
		h.svc.Disconnect()
	}

	h := newHarness(t, nil) // IPv6 off
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.tun.mu.Lock()
	defer h.tun.mu.Unlock()
	if h.tun.opts.Address6.IsValid() {
		t.Error("IPv6 off, yet the tunnel got an IPv6 address")
	}
}

// With IPv6 switched off in the system (Linux: disable_ipv6), the TUN
// interface cannot take an IPv6 address, so the tunnel is IPv4-only even
// with the setting on.
func TestIPv6TunnelSystemOff(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.IPv6 = true
		c.ipv6Off = func() bool { return true }
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.tun.mu.Lock()
	defer h.tun.mu.Unlock()
	if h.tun.opts.Address6.IsValid() {
		t.Errorf("IPv6 off in the system, yet the tunnel got %v", h.tun.opts.Address6)
	}
}

func TestTrafficEvents(t *testing.T) {
	h := newHarness(t, nil)
	// Xray cannot run hy2Link, sing-box can; the fake core answers its Clash API.
	if err := h.connect(t, hy2Link); err != nil {
		t.Fatal(err)
	}
	var last Event
	deadline := time.After(10 * time.Second)
	for last.Down < 2000 {
		select {
		case e := <-h.events:
			if e.Kind == kindTraffic {
				last = e
			}
		case <-deadline:
			t.Fatalf("no growing traffic, last %+v", last)
		}
	}
	if last.DownRate <= 0 || last.Up <= 0 {
		t.Errorf("traffic event %+v", last)
	}
	// The graph is replayed to a UI opened later, and forgotten on disconnect.
	replay, stop := h.svc.Subscribe(true)
	found := false
	for len(replay) > 0 {
		if e := <-replay; e.Kind == kindTraffic {
			found = true
		}
	}
	stop()
	if !found {
		t.Error("traffic not replayed")
	}
	h.svc.Disconnect()
	replay, stop = h.svc.Subscribe(true)
	defer stop()
	for len(replay) > 0 {
		if e := <-replay; e.Kind == kindTraffic {
			t.Fatal("traffic of an ended connection replayed")
		}
	}
}

// With the device idle the traffic is sampled seldom; back in use, at once.
func TestTrafficWhileIdle(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.trafficEvery, c.trafficIdleEvery = 50*time.Millisecond, 600*time.Millisecond })
	if err := h.connect(t, hy2Link); err != nil {
		t.Fatal(err)
	}
	count := func(d time.Duration) (n int) {
		end := time.After(d)
		for {
			select {
			case e := <-h.events:
				if e.Kind == kindTraffic {
					n++
				}
			case <-end:
				return n
			}
		}
	}
	if n := count(500 * time.Millisecond); n < 5 {
		t.Fatalf("%d traffic events in 0.5 s in use", n)
	}
	h.svc.SetBackground(true)
	if !h.svc.Background() {
		t.Fatal("not in the background")
	}
	count(100 * time.Millisecond) // a sample under way
	if n := count(time.Second); n > 2 {
		t.Errorf("%d traffic events in 1 s while idle, want 1 or 2", n)
	}
	// Just after an idle sample, the next would be 600 ms away.
	next := func() time.Time {
		for {
			select {
			case e := <-h.events:
				if e.Kind == kindTraffic {
					return time.Now()
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no traffic event")
			}
		}
	}
	next()
	h.svc.SetBackground(false)
	woke := time.Now()
	if d := next().Sub(woke); d > 300*time.Millisecond {
		t.Errorf("the first sample came %v after waking", d)
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.14.3", "1.14.2", true},
		{"1.14.2", "1.14.2", false},
		{"26.3.27", "26.10.1", false},
		{"26.10.1", "26.3.27", true},
		{"1.15.0", "1.15.0-beta.3", true},
		{"1.15.0-beta.3", "1.14.2", true},
		{"v1.19.31", "1.19.30", true},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestRoutingSettingsReachTheTunnel(t *testing.T) {
	set := store.Defaults()
	set.Routing.Mode = store.RouteSelected
	set.Routing.RussiaDirect = true
	set.Routing.ProxyDomains = []string{"youtube.com"}
	set.Routing.ProxyIPs = []string{"91.108.4.0/22", "203.0.113.9"}
	set.Routing.ProxyApps = []string{"Telegram.exe"}
	set.Routing.DirectIPs = []string{"10.8.0.0/16"}
	set.Routing.BlockDomains = []string{"ads.example"}
	o := OptionsFromSettings(set)
	fetched := false
	h := newHarness(t, func(c *Config) {
		c.Options.Selective, c.Options.ProxyDomains, c.Options.ProxyIPs = o.Selective, o.ProxyDomains, o.ProxyIPs
		c.Options.ProxyApps, c.Options.DirectIPs, c.Options.BlockDomains = o.ProxyApps, o.DirectIPs, o.BlockDomains
		c.DNS.RussiaDirect = o.DNS.RussiaDirect
		c.fetchRuleSet = func(context.Context, string, *url.URL) ([]byte, error) {
			fetched = true
			return nil, errors.New("not needed")
		}
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.tun.mu.Lock()
	got := h.tun.opts
	h.tun.mu.Unlock()
	if !got.Selective || !slices.Equal(got.DNS.ProxySuffixes, []string{"youtube.com"}) || !slices.Equal(got.DNS.BlockSuffixes, []string{"ads.example"}) ||
		!slices.Equal(got.ProxyApps, []string{"Telegram.exe"}) {
		t.Errorf("tun options = %+v", got)
	}
	wantIPs := []netip.Prefix{netip.MustParsePrefix("91.108.4.0/22"), netip.MustParsePrefix("203.0.113.9/32")}
	if !slices.Equal(got.ProxyIPs, wantIPs) || !slices.Equal(got.DirectIPs, []netip.Prefix{netip.MustParsePrefix("10.8.0.0/16")}) {
		t.Errorf("addresses = %v, %v", got.ProxyIPs, got.DirectIPs)
	}
	// Only the selected traffic uses the tunnel, so the Russian lists are
	// of no use.
	if fetched || len(got.DNS.DirectRuleSets) > 0 || slices.Contains(got.DNS.DirectSuffixes, "xn--p1ai") {
		t.Errorf("Russian preset applied in selective mode: fetched %v, %+v", fetched, got.DNS)
	}
}

func TestServerNamesBypassFakeIPs(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, "trojan://pw@server.example:443?sni=t.example.com#Named"); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	before := slices.Clone(h.lookups)
	h.mu.Unlock()
	if len(before) == 0 || before[0].IsValid() {
		t.Fatalf("before the tunnel the system resolver is right: %v", before)
	}

	// While connected, e.g. for the servers' pings, the tunnel would answer
	// the system resolver with fake addresses.
	if _, err := h.svc.serverAddr(context.Background(), "other.example"); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	last := h.lookups[len(h.lookups)-1]
	h.mu.Unlock()
	if last != netip.MustParseAddrPort("172.19.0.2:53") {
		t.Errorf("while connected, lookups went to %v, want the TUN layer's resolver", last)
	}

	h.svc.Disconnect()
	h.svc.serverAddr(context.Background(), "other.example")
	h.mu.Lock()
	last = h.lookups[len(h.lookups)-1]
	h.mu.Unlock()
	if last.IsValid() {
		t.Errorf("after disconnecting, lookups still go to %v", last)
	}
}

// Android keeps the app outside its VPN: the TUN layer's resolver is out of
// its reach, and the system's gives real addresses anyway.
func TestServerNamesOutsideTheVPN(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.AppOutsideVPN = true })
	if err := h.connect(t, "trojan://pw@server.example:443?sni=t.example.com#Named"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.serverAddr(context.Background(), "other.example"); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	last := h.lookups[len(h.lookups)-1]
	h.mu.Unlock()
	if last.IsValid() {
		t.Errorf("while connected, lookups went to %v, want the system's resolver", last)
	}
}

func TestNativeIPv6(t *testing.T) {
	for in, want := range map[string]bool{
		"2a02:6b8::1":                      true,
		"2001:0:c612:9:1435:764:53ec:fffe": false, // Teredo
		"2002:c000:204::1":                 false, // 6to4
		"fdfd::1a67:51dd":                  false, // unique local, e.g. Radmin VPN
		"fe80::1":                          false,
		"192.0.2.1":                        false,
	} {
		if got := nativeIPv6(netip.MustParseAddr(in)); got != want {
			t.Errorf("nativeIPv6(%s) = %v, want %v", in, got, want)
		}
	}
}

// TestLookupHostThroughServer resolves through a DNS server of the test's
// own, the way server names are looked up while connected.
func TestLookupHostThroughServer(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			q := buf[:n]
			// The question ends after the name's labels and type and
			// class; anything after it (the resolver's EDNS record) is
			// not echoed.
			end := 12
			for end < n && q[end] != 0 {
				end += int(q[end]) + 1
			}
			end += 5
			if end > n {
				continue
			}
			qtype := binary.BigEndian.Uint16(q[end-4:])
			// Header: same ID, response + recursion available, one
			// question; one answer only for A queries.
			resp := append([]byte{q[0], q[1], 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0}, q[12:end]...)
			if qtype == 1 {
				resp[7] = 1
				resp = append(resp, 0xc0, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 203, 0, 113, 77)
			}
			pc.WriteTo(resp, from)
		}
	}()
	server := netip.MustParseAddrPort(pc.LocalAddr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ip, err := lookupHost(ctx, "node9.example.test", server)
	if err != nil || ip != netip.MustParseAddr("203.0.113.77") {
		t.Fatalf("lookup = %v, %v", ip, err)
	}
}

func TestNetworkChangeReconnects(t *testing.T) {
	var mu sync.Mutex
	resolvers := []netip.Addr{netip.MustParseAddr("192.0.2.53")}
	set := func(a ...netip.Addr) { mu.Lock(); resolvers = a; mu.Unlock() }
	h := newHarness(t, func(c *Config) {
		c.netInterval = 10 * time.Millisecond
		c.resolvers = func(context.Context, string) ([]netip.Addr, error) {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(resolvers), nil
		}
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	starts := func() int { return countOf(h.log.get(), "tun.start") }

	// Offline for a moment: nothing to follow yet.
	set()
	time.Sleep(100 * time.Millisecond)
	// Another adapter joins; the resolver in use is still there.
	set(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.53"))
	time.Sleep(100 * time.Millisecond)
	if n := starts(); n != 1 {
		t.Fatalf("reconnected without a network change: %v", h.log.get())
	}

	set(netip.MustParseAddr("198.51.100.53"))
	deadline := time.Now().Add(5 * time.Second)
	for starts() < 2 || h.svc.Status().State != Connected {
		if time.Now().After(deadline) {
			t.Fatalf("no reconnect after the network changed: %v, %+v", h.log.get(), h.svc.Status())
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.tun.mu.Lock()
	direct := h.tun.opts.DNS.Direct
	h.tun.mu.Unlock()
	if direct != "198.51.100.53" {
		t.Errorf("direct DNS after the change = %s", direct)
	}

	// After a disconnect a network change is none of its business.
	h.svc.Disconnect()
	set(netip.MustParseAddr("203.0.113.53"))
	time.Sleep(100 * time.Millisecond)
	if n := starts(); n != 2 || h.svc.Status().State != Idle {
		t.Errorf("acted after disconnect: %d starts, %+v", n, h.svc.Status())
	}
}

func TestResolverOfAnotherTunnelIsSkipped(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.resolvers = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("172.19.0.2"), netip.MustParseAddr("192.0.2.53")}, nil
		}
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if d := h.tun.opts.DNS.Direct; d != "192.0.2.53" {
		t.Errorf("direct DNS = %s", d)
	}
}

func TestNoiseLines(t *testing.T) {
	for _, l := range []string{
		"+0400 2026-09-27 02:05:44 ERROR [612468618 83ms] dns: lookup failed for cookie.lmgssp.com: (exchange4: NXDOMAIN | exchange6: NXDOMAIN)",
		"+0400 2026-09-27 02:05:44 ERROR [612468618 84ms] router: lookup cookie.lmgssp.com: (exchange4: NXDOMAIN | exchange6: NXDOMAIN)",
		"ERROR [0012] [3012218906 12.22s] connection: connection download closed: close tcp [fdfe:dcba:9876::1]:43444->[fc00::78]:443: endpoint not connected",
		"ERROR [4901] [3143972750 0ms] connection: report handshake success: connection refused",
	} {
		if !noiseLine(l) {
			t.Errorf("not noise: %s", l)
		}
	}
	if noiseLine("ERROR [3035781270 5.30s] connection: open connection to 198.51.100.14:80 using outbound/direct[direct]: dial tcp 198.51.100.14:80: i/o timeout") {
		t.Error("a failed connection is not noise")
	}
}
