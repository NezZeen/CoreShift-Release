package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/subscription"
)

func newStoreAPI(t *testing.T, mutate func(*Config)) (*harness, *httptest.Server) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Settings fast enough for tests; the store starts with TUN on.
	set := st.Settings()
	set.Cores.HealthURL, set.Cores.HealthIntervalS, set.Cores.HealthFailures = "http://health.test/generate_204", 5, 2
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) {
		c.Store = st
		if mutate != nil {
			mutate(c)
		}
	})
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = NewAPI(h.svc, token, netip.MustParseAddrPort(srv.Listener.Addr().String()))
	srv.Start()
	t.Cleanup(srv.Close)
	return h, srv
}

func callJSON(t *testing.T, srv *httptest.Server, method, path string, body any, out any) int {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// hy2Link is a server xray cannot run: Xray no longer skips the certificate
// check.
const hy2Link = "hy2://auth@203.0.113.10:443/?sni=h.example.com&insecure=1#Helsinki"

func TestAPISubscriptionLifecycle(t *testing.T) {
	// A real panel over HTTP, so the whole fetch path runs.
	var gotUA string
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Profile-Title", "Test panel")
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=1000; expire=2000000000")
		w.Write([]byte(trojanLink + "\n" + hy2Link))
	}))
	defer panel.Close()
	h, srv := newStoreAPI(t, nil)

	var sub subscriptionView
	if code := callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"url": panel.URL + "/sub/TOKEN"}, &sub); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	if sub.DisplayName != "Test panel" || len(sub.Nodes) != 2 || sub.Info.Total != 1000 || sub.NextUpdate.IsZero() || gotUA == "" {
		t.Fatalf("sub = %+v, UA %q", sub, gotUA)
	}
	trojan, hy2 := sub.Nodes[0], sub.Nodes[1]
	if !slices.Equal(trojan.Cores, []core.Kind{core.Xray, core.SingBox, core.Mihomo}) ||
		!slices.Equal(hy2.Cores, []core.Kind{core.SingBox, core.Mihomo}) || hy2.Transport != "quic" {
		t.Errorf("nodes = %+v", sub.Nodes)
	}
	// The list view carries no credentials.
	raw := map[string]any{}
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &raw)
	if b, _ := json.Marshal(raw["nodes"]); strings.Contains(string(b), "auth") || strings.Contains(string(b), `"pw"`) {
		t.Errorf("credentials in the node list: %s", b)
	}

	if code := callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"url": panel.URL + "/sub/TOKEN"}, nil); code != http.StatusConflict {
		t.Errorf("duplicate: %d", code)
	}
	var e map[string]string
	if code := callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"url": "http://127.0.0.1:1/sub/SECRET"}, &e); code != http.StatusBadGateway ||
		strings.Contains(e["error"], "SECRET") {
		t.Errorf("unreachable panel: %d %v", code, e)
	}

	// Nothing selected yet.
	if code := callJSON(t, srv, "POST", "/v1/connect", nil, &e); code != http.StatusConflict {
		t.Errorf("connect without selection: %d %v", code, e)
	}
	var st Status
	if code := callJSON(t, srv, "POST", "/v1/connect", map[string]string{"subscription": sub.ID, "fingerprint": hy2.Fingerprint}, &st); code != http.StatusOK {
		t.Fatalf("connect: %d %+v", code, st)
	}
	if st.State != Connected || st.Node != "Helsinki" || st.Core != core.SingBox {
		t.Errorf("status = %+v", st)
	}
	var sel selectionView
	callJSON(t, srv, "GET", "/v1/selection", nil, &sel)
	if !sel.Available || sel.Name != "Helsinki" || sel.Node == nil || sel.Node.Fingerprint != hy2.Fingerprint {
		t.Errorf("selection = %+v", sel)
	}

	// Disconnect, then reconnect the saved selection with an empty body.
	callJSON(t, srv, "POST", "/v1/disconnect", nil, nil)
	if code := callJSON(t, srv, "POST", "/v1/connect", nil, &st); code != http.StatusOK || st.Node != "Helsinki" {
		t.Errorf("connect selected: %d %+v", code, st)
	}

	name := "Renamed"
	if code := callJSON(t, srv, "PATCH", "/v1/subscriptions/"+sub.ID, store.Edit{Name: &name}, &sub); code != http.StatusOK || sub.DisplayName != "Renamed" {
		t.Errorf("rename: %d %+v", code, sub)
	}
	if code := callJSON(t, srv, "POST", "/v1/subscriptions/"+sub.ID+"/refresh", nil, &sub); code != http.StatusOK || sub.LastError != "" {
		t.Errorf("refresh: %d %+v", code, sub)
	}
	if code := callJSON(t, srv, "DELETE", "/v1/subscriptions/"+sub.ID, nil, nil); code != http.StatusNoContent {
		t.Errorf("delete: %d", code)
	}
	if code := callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, nil); code != http.StatusNotFound {
		t.Errorf("get deleted: %d", code)
	}
	// Removing the subscription leaves the running connection alone.
	if st := h.svc.Status(); st.State != Connected {
		t.Errorf("status after delete = %+v", st)
	}
}

func TestAPISettings(t *testing.T) {
	h, srv := newStoreAPI(t, nil)
	var set store.Settings
	if code := callJSON(t, srv, "GET", "/v1/settings", nil, &set); code != http.StatusOK || !set.TUN {
		t.Fatalf("get: %d %+v", code, set)
	}

	var e map[string]string
	if code := callJSON(t, srv, "PUT", "/v1/settings", map[string]any{"tun": false, "dns": map[string]any{"remot": "x"}}, &e); code != http.StatusBadRequest ||
		!strings.Contains(e["error"], "remot") {
		t.Errorf("unknown field: %d %v", code, e)
	}
	bad := set
	bad.DNS.Remote = "gopher://dns"
	if code := callJSON(t, srv, "PUT", "/v1/settings", bad, &e); code != http.StatusBadRequest || !strings.Contains(e["error"], "dns.remote") {
		t.Errorf("invalid: %d %v", code, e)
	}

	// Connected with TUN; turning it off applies on reconnect.
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	set.TUN = false
	set.Routing.DirectDomains = []string{"RU"}
	var saved store.Settings
	if code := callJSON(t, srv, "PUT", "/v1/settings", set, &saved); code != http.StatusOK || saved.Routing.DirectDomains[0] != "ru" {
		t.Fatalf("put: %d %+v", code, saved)
	}
	var st Status
	callJSON(t, srv, "GET", "/v1/status", nil, &st)
	if !st.Pending || !st.TUN {
		t.Errorf("status after change = %+v", st)
	}
	before := len(h.log.get())
	st = Status{}
	if code := callJSON(t, srv, "POST", "/v1/reconnect", nil, &st); code != http.StatusOK {
		t.Fatalf("reconnect: %d", code)
	}
	if st.Pending || st.TUN || st.State != Connected {
		t.Errorf("status after reconnect = %+v", st)
	}
	if calls := h.log.get()[before:]; !slices.Contains(calls, "tun.stop") || slices.Contains(calls, "tun.start") {
		t.Errorf("reconnect calls = %v", calls)
	}
}

func TestSettingsApplyToNextConnection(t *testing.T) {
	h, _ := newStoreAPI(t, nil)
	st := h.svc.Store()
	set := st.Settings()
	set.Cores.Mode, set.Cores.Manual = store.ModeManual, core.Mihomo
	set.Routing.DirectDomains = []string{"example.ru"}
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	s := h.svc.Status()
	if s.Core != core.Mihomo || !slices.Equal(s.Chain, []core.Kind{core.Mihomo}) || s.Pending {
		t.Errorf("status = %+v", s)
	}
	h.tun.mu.Lock()
	opts := h.tun.opts
	h.tun.mu.Unlock()
	for _, want := range []string{"example.ru", "lan"} {
		if !slices.Contains(opts.DNS.DirectSuffixes, want) {
			t.Errorf("direct suffixes %v lack %s", opts.DNS.DirectSuffixes, want)
		}
	}
}

func TestTUNUnavailable(t *testing.T) {
	h, _ := newStoreAPI(t, func(c *Config) { c.TUNUnavailable = "needs administrator rights" })
	if err := h.connect(t, trojanLink); err == nil || !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("err = %v", err)
	}
	if calls := h.log.get(); slices.Contains(calls, "dns.apply") {
		t.Errorf("DNS touched: %v", calls)
	}
	st := h.svc.Store()
	set := st.Settings()
	set.TUN = false
	st.SetSettings(set)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatalf("SOCKS-only connect: %v", err)
	}
}

func TestAPIInfo(t *testing.T) {
	_, srv := newStoreAPI(t, func(c *Config) { delete(c.Binaries, core.SingBox) })
	var info struct {
		Version        string     `json:"version"`
		Cores          []coreInfo `json:"cores"`
		TUNAvailable   bool       `json:"tun_available"`
		TUNUnavailable string     `json:"tun_unavailable"`
	}
	if code := callJSON(t, srv, "GET", "/v1/info", nil, &info); code != http.StatusOK {
		t.Fatal(code)
	}
	installed := map[core.Kind]bool{}
	for _, c := range info.Cores {
		installed[c.Kind] = c.Installed
	}
	if info.Version == "" || !installed[core.Xray] || installed[core.SingBox] || info.TUNAvailable || !strings.Contains(info.TUNUnavailable, "sing-box") {
		t.Errorf("info = %+v", info)
	}
}

func TestAPILatency(t *testing.T) {
	h, srv := newStoreAPI(t, nil)
	set := h.svc.Store().Settings()
	set.Cores.LatencyTest = store.LatencyProxy
	if code := callJSON(t, srv, "PUT", "/v1/settings", set, nil); code != http.StatusOK {
		t.Fatalf("settings: %d", code)
	}
	for len(h.events) > 0 {
		<-h.events
	}
	var sub subscriptionView
	callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": trojanLink + "\n" + hy2Link}, &sub)

	var res []NodeLatency
	if code := callJSON(t, srv, "POST", "/v1/latency", map[string]string{"subscription": sub.ID}, &res); code != http.StatusOK || len(res) != 2 {
		t.Fatalf("latency: %d %+v", code, res)
	}
	for _, r := range res {
		if r.Error != "" || r.LatencyMS <= 0 {
			t.Errorf("result %+v", r)
		}
	}
	if res[1].Core != "sing-box" {
		t.Errorf("hysteria2 tested with %s", res[1].Core)
	}
	// The node list shows the results.
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &sub)
	if sub.Nodes[0].LatencyMS <= 0 {
		t.Errorf("node view lacks latency: %+v", sub.Nodes[0])
	}
	var events int
	for len(h.events) > 0 {
		if e := <-h.events; e.Kind == "latency" && e.Fingerprint != "" {
			events++
		}
	}
	if events != 2 {
		t.Errorf("%d latency events, want 2", events)
	}
	if code := callJSON(t, srv, "POST", "/v1/latency", map[string]string{"subscription": "nope"}, nil); code != http.StatusBadRequest {
		t.Errorf("unknown subscription: %d", code)
	}
}

func TestLatencyPing(t *testing.T) {
	h, srv := newStoreAPI(t, func(c *Config) {
		c.lookup = func(context.Context, string, netip.AddrPort) (netip.Addr, error) {
			return netip.MustParseAddr("198.51.100.7"), nil
		}
		phys := ping.Bind{Source: netip.MustParseAddr("192.168.1.10"), Interface: "eth0"}
		c.physical = func() (ping.Bind, error) { return phys, nil }
		c.icmpPing = func(_ context.Context, ip netip.Addr, b ping.Bind) (time.Duration, error) {
			if b != phys {
				return 0, errors.New("not sent from the physical interface")
			}
			switch ip.String() {
			case "198.51.100.7":
				return 40 * time.Millisecond, nil
			case "203.0.113.20":
				return 50 * time.Microsecond, nil // too fast: a local tunnel answered
			}
			return 0, errors.New("request timed out")
		}
		c.tcpPing = func(_ context.Context, ap netip.AddrPort, _ ping.Bind) (time.Duration, error) {
			switch {
			case ap.Addr().String() == "203.0.113.20":
				return 50 * time.Microsecond, nil // too fast: a local tunnel answered
			case ap.Port() == 443:
				return 60 * time.Millisecond, nil
			}
			return 0, errors.New("connection refused")
		}
	})
	links := []string{
		"trojan://pw@a.example.com:443?sni=a.example.com#TCP",
		"trojan://pw@203.0.113.6:8443?sni=t.example.com#Down",
		"trojan://pw@203.0.113.20:443?sni=t.example.com#Local",
		"hysteria2://pw@a.example.com:443?sni=a.example.com#UDP",
		"hysteria2://pw@203.0.113.9:443?sni=t.example.com#UDPNoICMP",
	}
	var sub subscriptionView
	callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": strings.Join(links, "\n")}, &sub)
	var res []NodeLatency
	if code := callJSON(t, srv, "POST", "/v1/latency", map[string]string{"subscription": sub.ID}, &res); code != http.StatusOK || len(res) != 5 {
		t.Fatalf("latency: %d %+v", code, res)
	}
	// Servers are timed by a handshake alone where it answers. What the
	// light probes cannot time goes through a core, which alone says a
	// server is down: a handshake refused, a tunnel on this computer
	// answering, a UDP server ignoring ICMP.
	want := []struct {
		method string
		ms     int64
		fails  bool
	}{{"tcp", 60, false}, {"proxy", 0, false}, {"proxy", 0, false}, {"icmp", 40, false}, {"proxy", 0, false}}
	for i, w := range want {
		r := res[i]
		if r.Method != w.method || (r.Error != "") != w.fails || (w.ms > 0 && r.LatencyMS != w.ms) || (!w.fails && r.LatencyMS <= 0) {
			t.Errorf("%s: %+v", links[i], r)
		}
	}
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &sub)
	if n := sub.Nodes[0]; n.LatencyMethod != "tcp" || n.LatencyMS != 60 {
		t.Errorf("node view: %+v", n)
	}
	_ = h
}

// latencyRig is a store API whose pings fail each server its own way:
// whatever the direct path says, the server is tested through a core.
func latencyRig(t *testing.T) (*harness, *httptest.Server, *sync.Map) {
	t.Helper()
	var dohAsked sync.Map
	h, srv := newStoreAPI(t, func(c *Config) {
		c.lookup = func(_ context.Context, host string, _ netip.AddrPort) (netip.Addr, error) {
			switch host {
			case "nodns.example.com", "dohonly.example.com":
				return netip.Addr{}, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			case "slowdns.example.com":
				return netip.Addr{}, &net.DNSError{Err: "i/o timeout", Name: host, IsTimeout: true}
			}
			return netip.MustParseAddr("198.51.100.7"), nil
		}
		c.dohLookup = func(_ context.Context, host string, _ ping.Bind) ([]netip.Addr, error) {
			dohAsked.Store(host, true)
			if host == "dohonly.example.com" {
				return []netip.Addr{netip.MustParseAddr("2001:db8::5"), netip.MustParseAddr("198.51.100.50")}, nil
			}
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		c.physical = func() (ping.Bind, error) { return ping.Bind{}, nil }
		c.icmpPing = func(context.Context, netip.Addr, ping.Bind) (time.Duration, error) {
			return 0, errors.New("destination host unreachable")
		}
		c.tcpPing = func(_ context.Context, ap netip.AddrPort, _ ping.Bind) (time.Duration, error) {
			switch ap.Port() {
			case 443:
				return 30 * time.Millisecond, nil
			case 1001:
				return 0, errors.New("dial tcp: i/o timeout")
			case 1002:
				return 0, errors.New("connectex: No connection could be made because the target machine actively refused it")
			}
			return 0, errors.New("connect: network is unreachable")
		}
	})
	return h, srv, &dohAsked
}

var latencyRigLinks = []string{
	"trojan://pw@nodns.example.com:443?sni=a.example.com#NoDNS",
	"trojan://pw@slowdns.example.com:443?sni=a.example.com#DNSTimeout",
	"trojan://pw@dohonly.example.com:443?sni=a.example.com#DoH",
	"trojan://pw@203.0.113.6:1001?sni=t.example.com#Timeout",
	"trojan://pw@203.0.113.6:1002?sni=t.example.com#Refused",
	"trojan://pw@203.0.113.6:1003?sni=t.example.com#Unreachable",
	"hysteria2://pw@203.0.113.9:443?sni=t.example.com#NoICMP",
}

// Every kind of direct-ping failure falls back to the test through a core:
// a provider may block the direct path, or fail the server's name, while
// the proxy gets through.
func TestLatencyPingFailuresGoThroughCore(t *testing.T) {
	_, srv, dohAsked := latencyRig(t)
	var sub subscriptionView
	callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": strings.Join(latencyRigLinks, "\n")}, &sub)
	var res []NodeLatency
	if code := callJSON(t, srv, "POST", "/v1/latency", map[string]string{"subscription": sub.ID}, &res); code != http.StatusOK || len(res) != len(latencyRigLinks) {
		t.Fatalf("latency: %d %+v", code, res)
	}
	for i, r := range res {
		want := methodProxy
		if i == 2 {
			// The system's resolver failed it, DNS over HTTPS did not: the
			// handshake with that address is timed.
			want = methodTCP
		}
		if r.Method != want || r.Error != "" || r.LatencyMS <= 0 {
			t.Errorf("%s: %+v, want %s", latencyRigLinks[i], r, want)
		}
	}
	for _, host := range []string{"nodns.example.com", "slowdns.example.com", "dohonly.example.com"} {
		if _, ok := dohAsked.Load(host); !ok {
			t.Errorf("%s not asked over DNS over HTTPS", host)
		}
	}
	if _, ok := dohAsked.Load("203.0.113.6"); ok {
		t.Error("an address was looked up")
	}
}

// A server is down only when the core cannot reach it either; the error
// says why both failed.
func TestLatencyPingAndCoreFail(t *testing.T) {
	for _, k := range []string{"XRAY", "SING_BOX", "MIHOMO"} {
		t.Setenv("FAKECORE_"+k, "unhealthy")
	}
	_, srv, _ := latencyRig(t)
	links := []string{latencyRigLinks[0], latencyRigLinks[4]}
	var sub subscriptionView
	callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": strings.Join(links, "\n")}, &sub)
	var res []NodeLatency
	if code := callJSON(t, srv, "POST", "/v1/latency", map[string]string{"subscription": sub.ID}, &res); code != http.StatusOK || len(res) != 2 {
		t.Fatalf("latency: %d %+v", code, res)
	}
	for i, want := range []string{"nodns.example.com", "TCP: connectex"} {
		r := res[i]
		if r.Method != methodProxy || r.LatencyMS != 0 || !strings.HasPrefix(r.Error, "ping: ") || !strings.Contains(r.Error, "; proxy: ") || !strings.Contains(r.Error, want) {
			t.Errorf("%s: %+v", links[i], r)
		}
	}
}

// In proxy mode every server goes through its core, as before; a name only
// DNS over HTTPS knows is handed to the core as an address.
func TestLatencyProxyMode(t *testing.T) {
	h, srv, _ := latencyRig(t)
	set := h.svc.Store().Settings()
	set.Cores.LatencyTest = store.LatencyProxy
	if code := callJSON(t, srv, "PUT", "/v1/settings", set, nil); code != http.StatusOK {
		t.Fatalf("settings: %d", code)
	}
	var sub subscriptionView
	callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": strings.Join(latencyRigLinks[:3], "\n")}, &sub)
	var res []NodeLatency
	if code := callJSON(t, srv, "POST", "/v1/latency", map[string]string{"subscription": sub.ID}, &res); code != http.StatusOK || len(res) != 3 {
		t.Fatalf("latency: %d %+v", code, res)
	}
	for i, r := range res {
		if r.Method != methodProxy || r.Error != "" || r.LatencyMS <= 0 {
			t.Errorf("%s: %+v", latencyRigLinks[i], r)
		}
	}
	nodes := make([]node.Node, 3)
	for i, l := range latencyRigLinks[:3] {
		nodes[i], _ = subscription.ParseLink(l)
	}
	if addrs := h.svc.resolveServers(context.Background(), nodes, nil); addrs[0] != "" || addrs[1] != "" || addrs[2] != "198.51.100.50" {
		t.Errorf("addresses for the cores: %q", addrs)
	}
}

// A system resolver that is slow to answer does not hold the test up:
// DNS over HTTPS is asked meanwhile, and the first answer wins.
func TestTestAddrAsksDoHWhileSystemIsSlow(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := newHarness(t, func(c *Config) {
		c.lookup = func(ctx context.Context, host string, _ netip.AddrPort) (netip.Addr, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return netip.Addr{}, errors.New("too late")
		}
		c.dohLookup = func(context.Context, string, ping.Bind) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("198.51.100.60")}, nil
		}
	})
	start := time.Now()
	ip, err := h.svc.testAddr(context.Background(), "slow.example.com", ping.Bind{})
	if err != nil || ip != netip.MustParseAddr("198.51.100.60") {
		t.Fatalf("ip %v, err %v", ip, err)
	}
	if d := time.Since(start); d < dohAfter || d > dohAfter+2*time.Second {
		t.Errorf("answered after %v", d)
	}
}

// Results of servers that are gone are forgotten when their subscription
// is tested again: the engine runs for days.
func TestLatencyForgetsServersThatLeft(t *testing.T) {
	h, srv := newStoreAPI(t, nil)
	var sub subscriptionView
	callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": trojanLink}, &sub)
	l := &h.svc.latency
	l.mu.Lock()
	l.results = map[string]NodeLatency{sub.ID + "/stale": {}, "other/kept": {}}
	l.mu.Unlock()
	has := func(k string) bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		_, ok := l.results[k]
		return ok
	}
	if _, err := h.svc.TestLatency(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	if has(sub.ID+"/stale") || !has("other/kept") || !has(sub.ID+"/"+sub.Nodes[0].Fingerprint) {
		t.Errorf("after testing the subscription: %v", l.results)
	}
	if _, err := h.svc.TestLatency(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if has("other/kept") || !has(sub.ID+"/"+sub.Nodes[0].Fingerprint) {
		t.Errorf("after testing them all: %v", l.results)
	}
}

func TestLatencyTestUsesResolvedServers(t *testing.T) {
	var mu sync.Mutex
	looked := map[string]int{}
	h, _ := newStoreAPI(t, func(c *Config) {
		c.lookup = func(_ context.Context, host string, _ netip.AddrPort) (netip.Addr, error) {
			mu.Lock()
			looked[host]++
			mu.Unlock()
			return netip.MustParseAddr("198.51.100.7"), nil
		}
	})
	nodes := []node.Node{}
	for _, l := range []string{
		"trojan://pw@a.example.com:443?sni=a.example.com#A",
		"trojan://pw@a.example.com:8443?sni=a.example.com#A2",
		"trojan://pw@203.0.113.5:443?sni=t.example.com#IP",
	} {
		n, _ := subscription.ParseLink(l)
		nodes = append(nodes, n)
	}
	addrs := h.svc.resolveServers(context.Background(), nodes, nil)
	if addrs[0] != "198.51.100.7" || addrs[1] != "198.51.100.7" || addrs[2] != "" {
		t.Errorf("addrs = %v", addrs)
	}
	if looked["a.example.com"] != 1 || len(looked) != 1 {
		t.Errorf("lookups = %v", looked)
	}
}

func TestDaemonGetsRealDNSThroughTUN(t *testing.T) {
	h, _ := newStoreAPI(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.tun.mu.Lock()
	defer h.tun.mu.Unlock()
	self, _ := os.Executable()
	if !slices.Contains(h.tun.opts.DirectDNSProcesses, self) {
		t.Errorf("daemon %s not in DirectDNSProcesses %v", self, h.tun.opts.DirectDNSProcesses)
	}
}

func TestAPICoresAndApps(t *testing.T) {
	h, srv := newStoreAPI(t, nil)
	var info struct {
		Cores []coreInfo `json:"cores"`
	}
	callJSON(t, srv, "GET", "/v1/info", nil, &info)
	for _, c := range info.Cores {
		if c.Version != "1.2.3" {
			t.Errorf("%s version %q", c.Kind, c.Version)
		}
	}
	var apps []map[string]string
	// As root on Linux the list holds only people's programs, and a test
	// machine may have none.
	rootOnLinux := runtime.GOOS == "linux" && os.Geteuid() == 0
	if code := callJSON(t, srv, "GET", "/v1/apps", nil, &apps); code != http.StatusOK || (len(apps) == 0 && !rootOnLinux) {
		t.Errorf("apps: %d, %v", code, apps)
	}
	var e map[string]any
	if code := callJSON(t, srv, "POST", "/v1/cores/return", nil, &e); code != http.StatusConflict {
		t.Errorf("return while idle: %d %v", code, e)
	}
	if code := callJSON(t, srv, "POST", "/v1/cores/v2ray/update", nil, &e); code != http.StatusNotFound {
		t.Errorf("unknown core update: %d", code)
	}
	_ = h
}

// Settings the connection does not use (auto-connect, subscription and
// app updates) do not ask for a reconnect, nor does a change undone.
func TestAPISettingsPendingOnlyForTheConnection(t *testing.T) {
	h, srv := newStoreAPI(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	var set store.Settings
	callJSON(t, srv, "GET", "/v1/settings", nil, &set)
	pending := func(what string, s store.Settings) bool {
		t.Helper()
		if code := callJSON(t, srv, "PUT", "/v1/settings", s, nil); code != http.StatusOK {
			t.Fatalf("%s: put %d", what, code)
		}
		var st Status
		callJSON(t, srv, "GET", "/v1/status", nil, &st)
		return st.Pending
	}

	other := set
	other.AutoConnect = !set.AutoConnect
	other.Updates.Auto = !set.Updates.Auto
	other.Updates.IntervalHours = set.Updates.IntervalHours + 5
	other.AppUpdate.Auto = !set.AppUpdate.Auto
	if pending("auto-connect and updates", other) {
		t.Error("auto-connect and updates ask for a reconnect")
	}
	frag := other
	frag.Cores.Fragment = !set.Cores.Fragment
	if !pending("fragment", frag) {
		t.Error("the fragment does not ask for a reconnect")
	}
	if pending("fragment undone", other) {
		t.Error("a change undone still asks for a reconnect")
	}
}
