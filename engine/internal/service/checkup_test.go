package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/store"
)

// checkupHarness is connected through the fake core, which reaches a fake
// bash.ws and a fake speed test server in its zone; mutate sets up the
// network around it.
func checkupHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	speed := speedServerFake(t, nil)
	// One server for both: the speed sample's path, else bash.ws.
	b := &fakeBash{seen: map[string][]string{}}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__down" {
			speed.Config.Handler.ServeHTTP(w, r)
			return
		}
		b.serve(w, r)
	}))
	t.Cleanup(b.srv.Close)
	t.Setenv("FAKECORE_FORWARD", "bash.test="+b.srv.Listener.Addr().String())
	h := newHarness(t, func(c *Config) {
		c.leakBase, c.leakDomain = "http://bash.test", "bash.test"
		c.leakHome = func(context.Context) (IPInfo, error) { return IPInfo{IP: "198.51.100.20", Country: "RU"}, nil }
		c.TUNLookup = fakeIPLookup
		c.speedURL = "http://speed.bash.test"
		if mutate != nil {
			mutate(c)
		}
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	return h
}

// up makes the hosts in addrs answer pings from the physical network.
func up(addrs ...string) func(*Config) {
	return func(c *Config) { network(false, addrs...)(c, nil) }
}

func stepsOf(r CheckupResult) map[string]CheckStep {
	m := map[string]CheckStep{}
	for _, s := range r.Steps {
		m[s.ID] = s
	}
	return m
}

func shortSample(t *testing.T) {
	d := speedSampleFor
	speedSampleFor = time.Second
	t.Cleanup(func() { speedSampleFor = d })
}

func TestCheckupWhenAllWorks(t *testing.T) {
	shortSample(t)
	h := checkupHarness(t, up("1.1.1.1:443", "8.8.8.8:443", "77.88.8.8:443", "203.0.113.5:443"))
	for len(h.events) > 0 {
		<-h.events
	}
	start := time.Now()
	res, err := h.svc.Checkup(context.Background(), CheckupOptions{Speed: true})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > checkupTimeout {
		t.Errorf("took %s", took)
	}
	var ids []string
	for _, s := range res.Steps {
		ids = append(ids, s.ID)
		if s.Status != CheckOK {
			t.Errorf("step %s: %s %q", s.ID, s.Status, s.Detail)
		}
		if s.Title == "" || s.Detail == "" {
			t.Errorf("step %+v has no title or detail", s)
		}
		// The app words it from the code; the Russian is the same words.
		if !msg.Has(s.Code) || s.detail().String() != s.Detail {
			t.Errorf("step %s: code %q, args %v, detail %q", s.ID, s.Code, s.Args, s.Detail)
		}
	}
	if !slices.Equal(ids, checkupOrder) {
		t.Errorf("steps %v, want %v", ids, checkupOrder)
	}
	if v := res.Verdict; v.Cause != "ok" || v.Status != CheckOK || v.Code != "checkup.verdict.ok" || v.Title != "Всё работает" {
		t.Errorf("verdict = %+v", v)
	}
	steps := stepsOf(res)
	if steps[stepTunnel].LatencyMS <= 0 || steps[stepSpeed].DownloadBps <= 0 {
		t.Errorf("tunnel %+v, speed %+v", steps[stepTunnel], steps[stepSpeed])
	}
	if !strings.Contains(steps[stepServer].Detail, "«Trojan»") || strings.Contains(steps[stepServer].Detail, "203.0.113.5") {
		t.Errorf("the server is named by its name only: %q", steps[stepServer].Detail)
	}
	if s := steps[stepServer]; s.Code != "checkup.server.ok" || s.Args["who"].(msg.Msg).Args["name"] != "Trojan" {
		t.Errorf("server step %+v", s)
	}
	if res.State != Connected || res.Server != "Trojan" {
		t.Errorf("result %+v", res)
	}

	// Each step was told as it finished, between the start and the verdict.
	var kinds []string
	told := map[string]string{}
	for len(h.events) > 0 {
		e := <-h.events
		if e.Kind != "checkup" {
			continue
		}
		kinds = append(kinds, e.Reason)
		if e.Reason == "step" {
			told[e.Step] = e.Status
			if !msg.Has(e.Code) || e.Line == "" {
				t.Errorf("step event %+v", e)
			}
		}
		if e.Reason == "done" && (e.Code != "checkup.verdict.ok.title" || e.Line != "Всё работает") {
			t.Errorf("done event %+v", e)
		}
	}
	if len(kinds) != len(checkupOrder)+2 || kinds[0] != "started" || kinds[len(kinds)-1] != "done" {
		t.Errorf("events %v", kinds)
	}
	if len(told) != len(checkupOrder) {
		t.Errorf("steps told: %v", told)
	}

	// Neither the result nor the events carry the SOCKS password.
	pass := h.svc.sup.SOCKSAuth().Pass
	out, _ := json.Marshal(res)
	if strings.Contains(string(out), pass) {
		t.Error("the result carries the SOCKS password")
	}
	for e := range drainEvents(h) {
		if strings.Contains(e, pass) {
			t.Errorf("an event carries the password: %s", e)
		}
	}
}

// Through the tunnel all is well, around it nothing answers, and the
// settings send something direct: the operator lets only a white list
// through, and everything should go through the VPN.
func TestCheckupFindsDirectConnectionsBlocked(t *testing.T) {
	h := checkupHarness(t, func(c *Config) {
		up("203.0.113.5:443")(c)
		c.DirectApps = []string{"game.exe"}
	})
	res, err := h.svc.Checkup(context.Background(), CheckupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	steps := stepsOf(res)
	if _, ok := steps[stepSpeed]; ok {
		t.Error("the speed sample ran unasked")
	}
	if steps[stepInternet].Status != CheckFail || steps[stepTunnel].Status != CheckOK || steps[stepDirect].Status != CheckFail {
		t.Errorf("steps %+v", res.Steps)
	}
	v := res.Verdict
	if v.Cause != "direct-blocked" || !slices.Equal(v.Actions, []string{actionRouting}) || v.Code != "checkup.verdict.direct_blocked" {
		t.Errorf("verdict = %+v", v)
	}
	if a, _ := v.Args["advice"].(msg.Msg); a.Code != "direct.advice.lists" {
		t.Errorf("advice %+v", v.Args["advice"])
	}
	if !strings.Contains(v.Advice, "Уберите из своих списков") {
		t.Errorf("advice in Russian: %q", v.Advice)
	}
	if s := steps[stepInternet]; s.Code != "checkup.internet.none" {
		t.Errorf("internet step %+v", s)
	}
}

// idleWithServer is not connected, with a subscription's server selected.
func idleWithServer(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) {
		c.Store = st
		if mutate != nil {
			mutate(c)
		}
	})
	sub, err := st.Add(context.Background(), store.AddRequest{Name: "s", Content: plainLinks})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Select(sub.ID, sub.Nodes[0].Fingerprint(), sub.Nodes[0].Name); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCheckupIdle(t *testing.T) {
	for name, c := range map[string]struct {
		mutate func(*Config)
		cause  string
		failed string // the step that failed, if any
	}{
		"the server does not answer": {mutate: up("1.1.1.1:443", "8.8.8.8:443"), cause: "server-down", failed: stepServer},
		"all answers":                {mutate: up("1.1.1.1:443", "203.0.113.1:443"), cause: "ready"},
		"no internet":                {mutate: up(), cause: "offline", failed: stepInternet},
		"only Yandex":                {mutate: up("77.88.8.8:443", "203.0.113.1:443"), cause: "whitelist"},
		"DNS does not answer": {mutate: func(c *Config) {
			up("1.1.1.1:443", "203.0.113.1:443")(c)
			c.lookup = func(context.Context, string, netip.AddrPort) (netip.Addr, error) {
				return netip.Addr{}, &net.DNSError{Err: "timeout", IsTimeout: true}
			}
		}, cause: "dns", failed: stepDNS},
	} {
		h := idleWithServer(t, c.mutate)
		res, err := h.svc.Checkup(context.Background(), CheckupOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Verdict.Cause != c.cause {
			t.Errorf("%s: verdict %+v; steps %+v", name, res.Verdict, res.Steps)
		}
		steps := stepsOf(res)
		for _, id := range []string{stepTunnel, stepTunnelDNS, stepLeak} {
			if steps[id].Status != CheckSkipped {
				t.Errorf("%s: %s = %+v while idle", name, id, steps[id])
			}
		}
		if c.failed != "" && steps[c.failed].Status != CheckFail {
			t.Errorf("%s: %s = %+v", name, c.failed, steps[c.failed])
		}
		if res.Server != "proxy" {
			t.Errorf("%s: server %q", name, res.Server)
		}
	}
}

func TestCheckupWithoutNetwork(t *testing.T) {
	h := idleWithServer(t, nil)
	h.offline.Store(true)
	res, err := h.svc.Checkup(context.Background(), CheckupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.Cause != "no-network" || stepsOf(res)[stepNetwork].Status != CheckFail {
		t.Errorf("result %+v", res)
	}
}

func TestCheckupVerdict(t *testing.T) {
	steps := func(kv ...string) map[string]CheckStep {
		m := map[string]CheckStep{}
		for i := 0; i+1 < len(kv); i += 2 {
			id, cause, _ := strings.Cut(kv[i], "/")
			m[id] = CheckStep{ID: id, Status: kv[i+1], cause: cause}
		}
		return m
	}
	for _, c := range []struct {
		state   State
		steps   map[string]CheckStep
		cause   string
		actions []string
	}{
		{Connected, steps(stepTunnel, CheckFail, stepServer, CheckOK, stepInternet, CheckOK), "server-blocked", []string{actionServers, actionReconnect}},
		{Connected, steps(stepTunnel, CheckFail, stepServer+"/port", CheckFail, stepInternet, CheckOK), "server-down", []string{actionServers, actionReconnect}},
		{Connected, steps(stepTunnel, CheckFail, stepServer, CheckWarn, stepInternet, CheckOK), "tunnel", []string{actionReconnect, actionServers}},
		{Connected, steps(stepTunnel, CheckFail, stepInternet, CheckFail), "offline", nil},
		{Connected, steps(stepTunnel, CheckOK, stepTunnelDNS, CheckFail), "tunnel-dns", []string{actionReconnect, actionServers}},
		{Connected, steps(stepTunnel, CheckOK, stepLeak, CheckFail), "leak", []string{actionLeak}},
		{Connected, steps(stepTunnel, CheckOK, stepDirect, CheckWarn), "whitelist", []string{actionRouting}},
		{Connected, steps(stepTunnel, CheckWarn), "slow", []string{actionServers}},
		{Connected, steps(stepTunnel, CheckOK, stepSpeed, CheckWarn), "slow", []string{actionServers}},
		// The network's DNS matters only to what goes direct.
		{Connected, steps(stepTunnel, CheckOK, stepDNS, CheckFail), "ok", nil},
		{Idle, steps(stepServer+"/resolve", CheckFail, stepInternet, CheckOK), "server-dns", []string{actionServers}},
		{Idle, steps(stepServer, CheckSkipped), "ready", []string{actionConnect}},
		{Failed, steps(stepServer, CheckOK), "connect-failed", []string{actionConnect, actionServers}},
	} {
		v := checkupVerdict(c.state, "Польша", c.steps, nil)
		if v.Cause != c.cause || !slices.Equal(v.Actions, c.actions) {
			t.Errorf("%s %v: %+v, want %s %v", c.state, c.steps, v, c.cause, c.actions)
		}
		if v.Title == "" || v.Advice == "" || v.Status == "" {
			t.Errorf("%s: %+v", c.cause, v)
		}
		if !msg.Has(v.Code+".title") || !msg.Has(v.Code+".advice") {
			t.Errorf("%s: code %q", c.cause, v.Code)
		}
	}
	// No server selected: said by the step's cause, not its words.
	if v := checkupVerdict(Idle, "", map[string]CheckStep{stepServer: skipStepNone()}, nil); v.Cause != "no-server" || v.Code != "checkup.verdict.no_server" {
		t.Errorf("no server: %+v", v)
	}
	// With the settings sending something direct the network's DNS counts.
	if v := checkupVerdict(Connected, "", steps(stepTunnel, CheckOK, stepDNS, CheckFail), []string{routeRussia}); v.Cause != "dns" {
		t.Errorf("verdict %+v", v)
	}
	v := checkupVerdict(Connected, "Польша", steps(stepTunnel, CheckFail, stepServer+"/port", CheckFail, stepInternet, CheckOK), nil)
	if !strings.Contains(v.Title, "«Польша»") {
		t.Errorf("title %q", v.Title)
	}
	if srv, _ := v.Args["srv"].(msg.Msg); v.Code != "checkup.verdict.server_down" || srv.Code != "checkup.srv" || srv.Args["name"] != "Польша" {
		t.Errorf("verdict %+v", v)
	}
}

// skipStepNone is the server step with no server selected.
func skipStepNone() CheckStep {
	s := (&Service{}).checkServer(context.Background(), node.Node{}, false, netip.Addr{})
	return s
}

func TestLeakVerdict(t *testing.T) {
	de := &LeakHost{IP: "203.0.113.5", Country: "de"}
	home := &IPInfo{IP: "198.51.100.20", Country: "RU"}
	for name, c := range map[string]struct {
		r    LeakResult
		want string
		isp  bool
	}{
		"the server's resolver": {LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "1", Country: "de", Path: "proxy"}}}, leakOK, false},
		"the ISP's resolver":    {LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "2", Country: "ru", Path: "system"}}}, leakFound, true},
		"a public resolver":     {LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "3", Country: "ru", Org: "Google LLC", Path: "system"}}}, leakOK, false},
		"another country":       {LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "4", Country: "fr", Path: "system"}}}, leakFound, false},
		"around the VPN":        {LeakResult{Exit: &LeakHost{IP: "198.51.100.77", Country: "ru"}, Home: home}, leakBypass, false},
		"no exit":               {LeakResult{Home: home}, leakUnknown, false},
		"no home":               {LeakResult{Exit: de, ServerIP: "203.0.113.9"}, leakUnknown, false},
		"no home, the server":   {LeakResult{Exit: de, ServerIP: "203.0.113.5"}, leakOK, false},
	} {
		if got, isp := leakVerdict(c.r); got != c.want || isp != c.isp {
			t.Errorf("%s: %s %v, want %s %v", name, got, isp, c.want, c.isp)
		}
	}
}

func TestCheckupOneAtATimeAndAPI(t *testing.T) {
	h, srv := newAPIServer(t)
	h.svc.checkupMu.Lock()
	if _, err := h.svc.Checkup(context.Background(), CheckupOptions{}); !errors.Is(err, ErrCheckupRunning) {
		t.Errorf("second checkup: %v", err)
	}
	if code, _ := call(t, srv, "POST", "/v1/checkup", "{}"); code != http.StatusConflict {
		t.Errorf("POST /v1/checkup while one runs: %d", code)
	}
	h.svc.checkupMu.Unlock()

	code, out := call(t, srv, "POST", "/v1/checkup", `{"speed": false}`)
	if code != http.StatusOK {
		t.Fatalf("POST /v1/checkup: %d %v", code, out)
	}
	if v, _ := out["verdict"].(map[string]any); v["cause"] == "" || v["title"] == "" {
		t.Errorf("verdict %v", out["verdict"])
	}
	if steps, _ := out["steps"].([]any); len(steps) != len(checkupOrder)-1 {
		t.Errorf("steps %v", out["steps"])
	}
	// The API's guard covers it like the rest.
	if code, _ := call(t, srv, "POST", "/v1/checkup", "", func(r *http.Request) { r.Header.Del("Authorization") }); code != http.StatusUnauthorized {
		t.Errorf("no token: %d", code)
	}
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	if code, _ := call(t, srv, "POST", "/v1/checkup", "", func(r *http.Request) { r.Host = "evil.example:" + port }); code != http.StatusForbidden {
		t.Errorf("foreign host: %d", code)
	}
	if code, _ := call(t, srv, "POST", "/v1/checkup", "{"); code != http.StatusBadRequest {
		t.Errorf("broken body: %d", code)
	}
}

// Cancelled, the checkup ends at once with the reason, whatever its steps
// were waiting for.
func TestCheckupCancelled(t *testing.T) {
	h := idleWithServer(t, func(c *Config) {
		c.tcpPing = func(ctx context.Context, _ netip.AddrPort, _ ping.Bind) (time.Duration, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	if _, err := h.svc.Checkup(ctx, CheckupOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("took %s after cancelling", took)
	}
}
