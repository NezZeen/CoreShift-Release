package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// What ipwho.is and ipinfo.io answer for the tester's server: bash.ws says
// Ukraine, they say Russia.
const (
	ipwhoRussia = `{"ip":"203.0.113.5","success":true,"country_code":"RU","country":"Russia","city":"Moscow","connection":{"asn":1,"org":"SERV.HOST GROUP","isp":"Serv Host"}}`
	ipwhoGerman = `{"ip":"203.0.113.5","success":true,"country_code":"DE","country":"Germany","city":"Frankfurt","connection":{"org":"Example Hosting"}}`
	ipinfoRus   = `{"ip":"203.0.113.5","city":"Moscow","region":"Moscow","country":"RU","org":"AS12345 SERV.HOST GROUP"}`
)

func reply(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }
}

func status(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		fmt.Fprint(w, body)
	}
}

func TestParseGeo(t *testing.T) {
	for name, c := range map[string]struct {
		parse func([]byte) (LeakGeo, error)
		body  string
		want  LeakGeo
		fails bool
	}{
		"ipwho.is": {parseIPWho, ipwhoRussia, LeakGeo{Country: "ru", CountryName: "Russia", City: "Moscow", Org: "SERV.HOST GROUP"}, false},
		"ipwho.is, the ISP when no organisation": {
			parseIPWho, `{"success":true,"country_code":"NL","country":"Netherlands","connection":{"isp":"Some ISP"}}`,
			LeakGeo{Country: "nl", CountryName: "Netherlands", Org: "Some ISP"}, false,
		},
		"ipwho.is, refused":               {parseIPWho, `{"success":false,"message":"You've hit the monthly limit"}`, LeakGeo{}, true},
		"ipwho.is, no country":            {parseIPWho, `{"success":true,"city":"Moscow"}`, LeakGeo{}, true},
		"ipwho.is, not JSON":              {parseIPWho, `<html>`, LeakGeo{}, true},
		"ipinfo.io":                       {parseIPInfo, ipinfoRus, LeakGeo{Country: "ru", City: "Moscow", Org: "SERV.HOST GROUP"}, false},
		"ipinfo.io, a bogon":              {parseIPInfo, `{"ip":"10.0.0.1","bogon":true}`, LeakGeo{}, true},
		"ipinfo.io, no country":           {parseIPInfo, `{"ip":"203.0.113.5","city":"X"}`, LeakGeo{}, true},
		"ipinfo.io, a limit message":      {parseIPInfo, `{"status":429,"error":{"title":"Rate limit exceeded"}}`, LeakGeo{}, true},
		"ipinfo.io, an organisation only": {parseIPInfo, `{"country":"DE","org":"Hosting GmbH"}`, LeakGeo{Country: "de", Org: "Hosting GmbH"}, false},
	} {
		got, err := c.parse([]byte(c.body))
		if (err != nil) != c.fails || got != c.want {
			t.Errorf("%s: %+v, %v; want %+v (fails %v)", name, got, err, c.want, c.fails)
		}
	}
}

// geoServer answers /<ip> of a source named by its path prefix: /a/<ip>.
func geoServer(t *testing.T, a, b http.HandlerFunc) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/a/") && a != nil:
			a(w, r)
		case strings.HasPrefix(r.URL.Path, "/b/") && b != nil:
			b(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestExitGeoFallsBack(t *testing.T) {
	for name, c := range map[string]struct {
		a, b    http.HandlerFunc
		want    string // source name and country, "" for none
		queries int
	}{
		"the first answers":                  {reply(ipwhoRussia), reply(ipinfoRus), "ipwho.is ru", 1},
		"the first is rate-limited":          {status(http.StatusTooManyRequests, `{"message":"limit"}`), reply(ipinfoRus), "ipinfo.io ru", 2},
		"the first refuses with a 200":       {reply(`{"success":false,"message":"limit"}`), reply(ipinfoRus), "ipinfo.io ru", 2},
		"the first is down":                  {status(http.StatusBadGateway, ""), reply(ipinfoRus), "ipinfo.io ru", 2},
		"the first knows no country":         {reply(`{"success":true}`), reply(ipinfoRus), "ipinfo.io ru", 2},
		"the first is gone":                  {nil, reply(ipinfoRus), "ipinfo.io ru", 2},
		"both fail":                          {status(http.StatusTooManyRequests, ""), status(http.StatusTooManyRequests, ""), "", 2},
		"both are gone":                      {nil, nil, "", 2},
		"the first answers, another country": {reply(ipwhoGerman), nil, "ipwho.is de", 1},
	} {
		srv, seen := geoServer(t, c.a, c.b)
		s := &Service{cfg: Config{leakGeo: []exitGeoSource{
			{"ipwho.is", srv.URL + "/a/%s", parseIPWho},
			{"ipinfo.io", srv.URL + "/b/%s", parseIPInfo},
		}}}
		g := s.exitGeo(context.Background(), srv.Client(), "203.0.113.5")
		got := ""
		if g != nil {
			got = g.Source + " " + g.Country
		}
		if got != c.want || len(*seen) != c.queries {
			t.Errorf("%s: %q after %v, want %q after %d requests", name, got, *seen, c.want, c.queries)
		}
		for _, p := range *seen {
			if !strings.HasSuffix(p, "/203.0.113.5") {
				t.Errorf("%s: asked %s", name, p)
			}
		}
	}
}

// A service that does not answer in time is given up on after the timeout,
// and the next one is asked.
func TestExitGeoTimeout(t *testing.T) {
	old := leakGeoTimeout
	leakGeoTimeout = 100 * time.Millisecond
	defer func() { leakGeoTimeout = old }()
	slow := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}
	srv, _ := geoServer(t, slow, reply(ipinfoRus))
	s := &Service{cfg: Config{leakGeo: []exitGeoSource{
		{"ipwho.is", srv.URL + "/a/%s", parseIPWho},
		{"ipinfo.io", srv.URL + "/b/%s", parseIPInfo},
	}}}
	start := time.Now()
	g := s.exitGeo(context.Background(), srv.Client(), "203.0.113.5")
	if g == nil || g.Source != "ipinfo.io" || time.Since(start) > 2*time.Second {
		t.Errorf("geo = %+v after %v", g, time.Since(start))
	}
}

// Only a public address goes to the services.
func TestExitGeoOnlyAPublicAddress(t *testing.T) {
	srv, seen := geoServer(t, reply(ipwhoRussia), reply(ipinfoRus))
	s := &Service{cfg: Config{leakGeo: []exitGeoSource{{"ipwho.is", srv.URL + "/a/%s", parseIPWho}}}}
	for _, ip := range []string{"", "not an address", "192.168.1.7", "10.1.2.3", "127.0.0.1", "169.254.1.1", "fd00::1", "::", "224.0.0.1"} {
		if g := s.exitGeo(context.Background(), srv.Client(), ip); g != nil {
			t.Errorf("%q was looked up: %+v", ip, g)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("asked %v", *seen)
	}
	if g := s.exitGeo(context.Background(), srv.Client(), "2001:db8::5"); g == nil || len(*seen) != 1 {
		t.Errorf("an IPv6 address: %+v, %v", g, *seen)
	}
}

func geoSources() []exitGeoSource {
	return []exitGeoSource{
		{"ipwho.is", "http://geo1.test/%s", parseIPWho},
		{"ipinfo.io", "http://geo2.test/%s", parseIPInfo},
	}
}

func noLookup(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("198.18.0.7")}, nil
}

// The geolocation services are reached through the core, like bash.ws:
// geo1.test and geo2.test resolve nowhere else, so a request that went
// around the core would fail. Only the exit address is asked about.
func TestLeakTestSecondGeolocation(t *testing.T) {
	for name, c := range map[string]struct {
		geo     map[string]http.HandlerFunc
		want    string // "source country city org", "" for none
		queries []string
	}{
		"the sources agree": {
			geo:     map[string]http.HandlerFunc{"geo1.test": reply(ipwhoGerman)},
			want:    "ipwho.is de Frankfurt Example Hosting",
			queries: []string{"geo1.test/" + vpnExit},
		},
		"the sources differ": {
			geo:     map[string]http.HandlerFunc{"geo1.test": reply(ipwhoRussia)},
			want:    "ipwho.is ru Moscow SERV.HOST GROUP",
			queries: []string{"geo1.test/" + vpnExit},
		},
		"the first fails, the second answers": {
			geo: map[string]http.HandlerFunc{
				"geo1.test": status(http.StatusTooManyRequests, `{"message":"limit"}`),
				"geo2.test": reply(ipinfoRus),
			},
			want:    "ipinfo.io ru Moscow SERV.HOST GROUP",
			queries: []string{"geo1.test/" + vpnExit, "geo2.test/" + vpnExit},
		},
		"both fail": {
			geo: map[string]http.HandlerFunc{
				"geo1.test": status(http.StatusServiceUnavailable, ""),
				"geo2.test": reply(`{"error":"x"}`),
			},
			want:    "",
			queries: []string{"geo1.test/" + vpnExit, "geo2.test/" + vpnExit},
		},
		"neither is reachable": {
			geo:     map[string]http.HandlerFunc{},
			want:    "",
			queries: nil,
		},
	} {
		h, b := leakGeoHarness(t, noLookup, c.geo, geoSources(), "198.51.100.20")
		res, err := h.svc.LeakTest(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// The test is not spoilt by it either way.
		if res.Verdict != leakOK || res.Exit == nil || res.Exit.Country != "de" {
			t.Errorf("%s: verdict %q, exit %+v", name, res.Verdict, res.Exit)
		}
		got := ""
		if g := res.ExitGeo; g != nil {
			got = g.Source + " " + g.Country + " " + g.City + " " + g.Org
		}
		if got != c.want {
			t.Errorf("%s: second source %q, want %q", name, got, c.want)
		}
		seen := b.geoRequests()
		if strings.Join(seen, " ") != strings.Join(c.queries, " ") {
			t.Errorf("%s: asked %v, want %v", name, seen, c.queries)
		}
		for _, q := range seen {
			if strings.Contains(q, "198.51.100.20") {
				t.Errorf("%s: the device's address went to %s", name, q)
			}
		}
	}
}

// The device's own address never goes to the second service: not when the
// test went around the VPN, not when nothing vouches that it went through,
// and not for the checkup, which does not show it.
func TestLeakTestSecondGeolocationNeverTheHome(t *testing.T) {
	geo := map[string]http.HandlerFunc{"geo1.test": reply(ipwhoRussia), "geo2.test": reply(ipinfoRus)}

	// bash.ws saw the device's address: around the VPN.
	h, b := leakGeoHarness(t, noLookup, geo, geoSources(), vpnExit)
	res, err := h.svc.LeakTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != leakBypass || res.ExitGeo != nil || len(b.geoRequests()) != 0 {
		t.Errorf("bypass: verdict %q, geo %+v, asked %v", res.Verdict, res.ExitGeo, b.geoRequests())
	}

	// The device's address is unknown and the exit is not the server's.
	h, b = leakGeoHarness(t, noLookup, geo, geoSources(), "198.51.100.20")
	h.svc.cfg.leakHome = func(context.Context) (IPInfo, error) { return IPInfo{}, fmt.Errorf("offline") }
	h.svc.mu.Lock()
	h.svc.lastNode.Server = "other.test"
	h.svc.mu.Unlock()
	if res, err = h.svc.LeakTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res.Verdict != leakUnknown || res.ExitGeo != nil || len(b.geoRequests()) != 0 {
		t.Errorf("unknown: verdict %q, geo %+v, asked %v", res.Verdict, res.ExitGeo, b.geoRequests())
	}

	// The checkup does not look the exit up.
	h, b = leakGeoHarness(t, noLookup, geo, geoSources(), "198.51.100.20")
	if res, err = h.svc.leakTest(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if res.ExitGeo != nil || len(b.geoRequests()) != 0 {
		t.Errorf("checkup: geo %+v, asked %v", res.ExitGeo, b.geoRequests())
	}
}

// The result carries the second source as exit_geo, and leaves it out when
// there is none.
func TestLeakGeoJSON(t *testing.T) {
	r := LeakResult{DNS: []LeakHost{}, ExitGeo: &LeakGeo{Source: "ipwho.is", Country: "ru", CountryName: "Russia", City: "Moscow", Org: "X"}}
	b, _ := json.Marshal(r)
	out := string(b)
	for _, want := range []string{`"exit_geo":{"source":"ipwho.is","country":"ru","country_name":"Russia","city":"Moscow","org":"X"}`} {
		if !strings.Contains(out, want) {
			t.Errorf("%s lacks %s", out, want)
		}
	}
	r.ExitGeo = nil
	if b, _ = json.Marshal(r); strings.Contains(string(b), "exit_geo") {
		t.Errorf("%s", b)
	}
}
