package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/supervisor"
)

// fakeBash imitates bash.ws in the zone bash.test. It is reached only
// through the core (FAKECORE_FORWARD): bash.test resolves nowhere else, so
// a request that went around the core fails.
type fakeBash struct {
	srv *httptest.Server

	mu   sync.Mutex
	ids  int
	seen map[string][]string // test id -> DNS servers that looked its names up
	hits int
}

const (
	// serverResolver looks up names for the VPN server, ispResolver for
	// the user's ISP.
	serverResolver = "203.0.113.53"
	ispResolver    = "198.51.100.53"
	vpnExit        = "203.0.113.5"
)

func newFakeBash(t *testing.T) *fakeBash {
	t.Helper()
	b := &fakeBash{seen: map[string][]string{}}
	b.srv = httptest.NewServer(http.HandlerFunc(b.serve))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *fakeBash) serve(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hits++
	host, _, _ := net.SplitHostPort(r.Host)
	if host == "" {
		host = r.Host
	}
	switch {
	case host == "bash.test" && r.URL.Path == "/id":
		b.ids++
		fmt.Fprintf(w, "test%04d\n", b.ids)
	case host == "bash.test" && strings.HasPrefix(r.URL.Path, "/dnsleak/test/"):
		id := strings.TrimPrefix(r.URL.Path, "/dnsleak/test/")
		if len(b.seen[id]) == 0 {
			fmt.Fprint(w, `{"error":"No DNS servers found. Try again..."}`)
			return
		}
		entries := []map[string]string{{"ip": vpnExit, "country": "de", "country_name": "Germany", "asn": "AS64500 Hosting", "type": "ip"}}
		for _, ip := range b.seen[id] {
			cc, name, org := "de", "Germany", "AS64500 Hosting"
			if ip == ispResolver {
				cc, name, org = "ru", "Russian Federation", "AS64501 Home ISP"
			}
			entries = append(entries, map[string]string{"ip": ip, "country": cc, "country_name": name, "asn": org, "type": "dns"})
		}
		entries = append(entries, map[string]string{"ip": "DNS may be leaking.", "type": "conclusion"})
		json.NewEncoder(w).Encode(entries)
	case strings.HasSuffix(host, ".bash.test"):
		// <n>.<id>.bash.test: the core sent the name on, so the server's
		// resolver looked it up.
		b.lookedUp(host, serverResolver)
		http.Redirect(w, r, "https://bash.test/", http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

func (b *fakeBash) lookedUp(name, resolver string) {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return
	}
	id := parts[1]
	for _, r := range b.seen[id] {
		if r == resolver {
			return
		}
	}
	b.seen[id] = append(b.seen[id], resolver)
}

func (b *fakeBash) requests() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hits
}

// leakHarness is connected through the fake core, which reaches the fake
// bash.ws; lookup answers the apps' path.
func leakHarness(t *testing.T, lookup func(ctx context.Context, host string) ([]netip.Addr, error)) (*harness, *fakeBash) {
	t.Helper()
	b := newFakeBash(t)
	t.Setenv("FAKECORE_FORWARD", "bash.test="+b.srv.Listener.Addr().String())
	h := newHarness(t, func(c *Config) {
		c.leakBase, c.leakDomain = "http://bash.test", "bash.test"
		c.leakHome = func(context.Context) (IPInfo, error) { return IPInfo{IP: "95.31.18.119", Country: "RU"}, nil }
		c.TUNLookup = lookup
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	return h, b
}

// fakeIPLookup answers as the tunnel does with fake IP: by itself.
func fakeIPLookup(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("198.18.0.7")}, nil
}

func TestLeakTestGoesThroughTheCore(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	h, b := leakHarness(t, func(ctx context.Context, host string) ([]netip.Addr, error) {
		mu.Lock()
		asked = append(asked, host)
		mu.Unlock()
		return fakeIPLookup(ctx, host)
	})

	res, err := h.svc.LeakTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Exit == nil || res.Exit.IP != vpnExit || res.Exit.Country != "de" {
		t.Errorf("exit = %+v, want the VPN server", res.Exit)
	}
	// The names went to the core by name, and the server resolved them.
	if len(res.DNS) != 1 || res.DNS[0].IP != serverResolver || res.DNS[0].Path != "proxy" {
		t.Errorf("dns = %+v, want only the server's resolver", res.DNS)
	}
	// The apps' path: the tunnel answered with fake addresses.
	if !res.System.Checked || !res.System.FakeIP || res.System.Error != "" {
		t.Errorf("system = %+v", res.System)
	}
	mu.Lock()
	if len(asked) != leakNames || !strings.HasSuffix(asked[0], ".bash.test") {
		t.Errorf("apps' path asked %v", asked)
	}
	mu.Unlock()
	if res.Home == nil || res.Home.IP != "95.31.18.119" || res.ServerIP != "203.0.113.5" || res.Server != "Trojan" {
		t.Errorf("home %+v, server %q %q", res.Home, res.Server, res.ServerIP)
	}

	// Nothing the test returns or reports carries the SOCKS password.
	auth := h.svc.sup.SOCKSAuth()
	out, _ := json.Marshal(res)
	if strings.Contains(string(out), auth.Pass) {
		t.Error("the result carries the SOCKS password")
	}
	for e := range drainEvents(h) {
		if strings.Contains(e, auth.Pass) {
			t.Errorf("an event carries the password: %s", e)
		}
	}

	// Without the credentials the core refuses, and bash.ws hears nothing.
	before := b.requests()
	wrong := &url.URL{Scheme: "socks5", Host: h.listen.String(), User: url.UserPassword(auth.User, "wrong")}
	tr := newTransport(wrong)
	defer tr.CloseIdleConnections()
	p := leakProbe{base: "http://bash.test", zone: "bash.test", client: &http.Client{Transport: tr}}
	if _, err := p.run(context.Background()); err == nil {
		t.Error("the test ran with wrong SOCKS credentials")
	}
	if b.requests() != before {
		t.Error("bash.ws was reached without the credentials")
	}
}

// When the apps' lookups reach the ISP's resolver, the result says so.
func TestLeakTestSeesTheISPResolver(t *testing.T) {
	var b *fakeBash
	h, b := leakHarness(t, func(_ context.Context, host string) ([]netip.Addr, error) {
		b.mu.Lock()
		b.lookedUp(host, ispResolver)
		b.mu.Unlock()
		return []netip.Addr{netip.MustParseAddr("192.0.2.80")}, nil
	})
	res, err := h.svc.LeakTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var system, proxy []string
	for _, d := range res.DNS {
		if d.Path == "system" {
			system = append(system, d.IP+" "+d.Country)
		} else {
			proxy = append(proxy, d.IP)
		}
	}
	if len(system) != 1 || system[0] != ispResolver+" ru" || len(proxy) != 1 || proxy[0] != serverResolver {
		t.Errorf("system %v, proxy %v", system, proxy)
	}
	if !res.System.Checked || res.System.FakeIP {
		t.Errorf("system = %+v, want real addresses", res.System)
	}
}

// A lookup on the apps' path that fails leaves the rest of the test.
func TestLeakTestSystemLookupFails(t *testing.T) {
	h, _ := leakHarness(t, func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("i/o timeout")
	})
	res, err := h.svc.LeakTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.System.Checked || res.System.Error != "i/o timeout" || res.Exit == nil {
		t.Errorf("result = %+v", res)
	}
}

// In proxy-only mode the apps' path is not the tunnel's: it is left out.
func TestLeakTestWithoutTUN(t *testing.T) {
	b := newFakeBash(t)
	t.Setenv("FAKECORE_FORWARD", "bash.test="+b.srv.Listener.Addr().String())
	called := false
	h := newHarness(t, func(c *Config) {
		c.TUN = false
		c.leakBase, c.leakDomain = "http://bash.test", "bash.test"
		c.leakHome = func(context.Context) (IPInfo, error) { return IPInfo{}, errors.New("offline") }
		c.TUNLookup = func(context.Context, string) ([]netip.Addr, error) { called = true; return nil, nil }
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.LeakTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if called || res.System.Checked || res.Home != nil || len(res.DNS) != 1 {
		t.Errorf("result = %+v, lookup called %v", res, called)
	}
}

func TestLeakTestNeedsAConnection(t *testing.T) {
	h, srv := newAPIServer(t)
	if _, err := h.svc.LeakTest(context.Background()); !errors.Is(err, supervisor.ErrNotConnected) {
		t.Errorf("err = %v", err)
	}
	if code, _ := call(t, srv, "POST", "/v1/leaktest", ""); code != http.StatusConflict {
		t.Errorf("POST /v1/leaktest while idle: %d", code)
	}
	// The API's guard covers it like the rest.
	if code, _ := call(t, srv, "POST", "/v1/leaktest", "", func(r *http.Request) { r.Header.Del("Authorization") }); code != http.StatusUnauthorized {
		t.Errorf("no token: %d", code)
	}
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	if code, _ := call(t, srv, "POST", "/v1/leaktest", "", func(r *http.Request) { r.Host = "evil.example:" + port }); code != http.StatusForbidden {
		t.Errorf("foreign host: %d", code)
	}
}

func TestLeakResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dnsleak/test/none":
			fmt.Fprint(w, `{"error":"No DNS servers found. Try again..."}`)
		case "/dnsleak/test/broken":
			fmt.Fprint(w, `{"error":"Service unavailable"}`)
		default:
			fmt.Fprint(w, `[{"ip":"1.2.3.4","country":"DE","country_name":"Germany","asn":"AS1 X","org":"","type":"ip"}]`)
		}
	}))
	defer srv.Close()
	p := leakProbe{base: srv.URL, client: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e, err := p.results(ctx, "none"); err != nil || e != nil {
		t.Errorf("no lookups: %v, %v", e, err)
	}
	if _, err := p.results(ctx, "broken"); err == nil || !strings.Contains(err.Error(), "Service unavailable") {
		t.Errorf("error answer: %v", err)
	}
	e, err := p.results(ctx, "ok")
	if err != nil || len(e) != 1 || e[0].host("").Country != "de" || e[0].host("").Org != "AS1 X" {
		t.Errorf("entries: %+v, %v", e, err)
	}
}

func TestSystemVerdict(t *testing.T) {
	fake, real := netip.MustParseAddr("198.18.3.4"), netip.MustParseAddr("93.184.216.34")
	timeout := errors.New("timeout")
	for _, c := range []struct {
		answers [][]netip.Addr
		errs    []error
		want    LeakSystem
	}{
		{[][]netip.Addr{{fake}, {fake}}, []error{nil, nil}, LeakSystem{Checked: true, FakeIP: true}},
		{[][]netip.Addr{{fake}, {real}}, []error{nil, nil}, LeakSystem{Checked: true}},
		{[][]netip.Addr{nil, {fake}}, []error{timeout, nil}, LeakSystem{Checked: true, FakeIP: true}},
		{[][]netip.Addr{nil, nil}, []error{timeout, timeout}, LeakSystem{Error: "timeout"}},
		{[][]netip.Addr{nil}, []error{nil}, LeakSystem{Error: "no answer"}},
	} {
		if got := systemVerdict(c.answers, c.errs); got != c.want {
			t.Errorf("%v %v: got %+v, want %+v", c.answers, c.errs, got, c.want)
		}
	}
}
