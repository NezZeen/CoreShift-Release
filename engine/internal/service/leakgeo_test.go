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

// What each service answers for an address in the country cc.
func ipwhoAns(cc, city, org string) string {
	return fmt.Sprintf(`{"success":true,"country_code":%q,"country":"Name of %s","city":%q,"connection":{"org":%q}}`, strings.ToUpper(cc), cc, city, org)
}
func ipinfoAns(cc, city, org string) string {
	return fmt.Sprintf(`{"country":%q,"city":%q,"org":"AS1 %s"}`, strings.ToUpper(cc), city, org)
}
func geojsAns(cc, city, org string) string {
	return fmt.Sprintf(`{"country_code":%q,"country":"Name of %s","city":%q,"organization_name":%q}`, strings.ToUpper(cc), cc, city, org)
}
func freeAns(cc, city, org string) string {
	return fmt.Sprintf(`{"countryCode":%q,"countryName":"Name of %s","cityName":%q,"asnOrganization":%q}`, strings.ToUpper(cc), cc, city, org)
}
func ipapiAns(cc, city, org string) string {
	return fmt.Sprintf(`{"country_code":%q,"country_name":"Name of %s","city":%q,"org":%q}`, strings.ToUpper(cc), cc, city, org)
}

func reply(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }
}

func status(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		fmt.Fprint(w, body)
	}
}

// geoTable is the five real sources' parsers under their names, at the
// addresses base(1) ... base(5).
func geoTable(base func(i int) string) []exitGeoSource {
	parsers := []func([]byte) (LeakGeoSource, error){parseIPWho, parseIPInfo, parseGeoJS, parseFreeIPAPI, parseIPAPICo}
	names := []string{"ipwho.is", "ipinfo.io", "geojs.io", "freeipapi", "ipapi.co"}
	out := make([]exitGeoSource, len(parsers))
	for i := range parsers {
		out[i] = exitGeoSource{names[i], base(i+1) + "/%s", parsers[i]}
	}
	return out
}

func TestParseGeo(t *testing.T) {
	for name, c := range map[string]struct {
		parse func([]byte) (LeakGeoSource, error)
		body  string
		want  LeakGeoSource
		fails bool
	}{
		"ipwho.is":                        {parseIPWho, `{"success":true,"country_code":"RU","country":"Russia","city":"Moscow","connection":{"asn":1,"org":"SERV.HOST GROUP","isp":"Serv Host"}}`, LeakGeoSource{Country: "ru", CountryName: "Russia", City: "Moscow", Org: "SERV.HOST GROUP"}, false},
		"ipwho.is, the ISP when no org":   {parseIPWho, `{"success":true,"country_code":"NL","country":"Netherlands","connection":{"isp":"Some ISP"}}`, LeakGeoSource{Country: "nl", CountryName: "Netherlands", Org: "Some ISP"}, false},
		"ipwho.is, refused":               {parseIPWho, `{"success":false,"message":"limit"}`, LeakGeoSource{}, true},
		"ipwho.is, no country":            {parseIPWho, `{"success":true,"city":"Moscow"}`, LeakGeoSource{}, true},
		"ipwho.is, not JSON":              {parseIPWho, `<html>`, LeakGeoSource{}, true},
		"ipinfo.io":                       {parseIPInfo, `{"city":"Moscow","country":"RU","org":"AS12345 SERV.HOST GROUP"}`, LeakGeoSource{Country: "ru", City: "Moscow", Org: "SERV.HOST GROUP"}, false},
		"ipinfo.io, a bogon":              {parseIPInfo, `{"ip":"10.0.0.1","bogon":true}`, LeakGeoSource{}, true},
		"ipinfo.io, a limit message":      {parseIPInfo, `{"status":429,"error":{"title":"Rate limit exceeded"}}`, LeakGeoSource{}, true},
		"ipinfo.io, an org only":          {parseIPInfo, `{"country":"DE","org":"Hosting GmbH"}`, LeakGeoSource{Country: "de", Org: "Hosting GmbH"}, false},
		"geojs.io":                        {parseGeoJS, `{"country_code":"RU","country":"Russia","city":"Moscow","organization_name":"SERV.HOST","organization":"AS1 SERV.HOST"}`, LeakGeoSource{Country: "ru", CountryName: "Russia", City: "Moscow", Org: "SERV.HOST"}, false},
		"geojs.io, the long organisation": {parseGeoJS, `{"country_code":"US","organization":"AS15169 Google LLC"}`, LeakGeoSource{Country: "us", Org: "Google LLC"}, false},
		"geojs.io, nothing known":         {parseGeoJS, `{"country_code":"","country":""}`, LeakGeoSource{}, true},
		"freeipapi":                       {parseFreeIPAPI, `{"countryCode":"RU","countryName":"Russia","cityName":"Moscow","asnOrganization":"SERV.HOST"}`, LeakGeoSource{Country: "ru", CountryName: "Russia", City: "Moscow", Org: "SERV.HOST"}, false},
		"freeipapi, a code of one letter": {parseFreeIPAPI, `{"countryCode":"R"}`, LeakGeoSource{}, true},
		"freeipapi, an array":             {parseFreeIPAPI, `[1,2]`, LeakGeoSource{}, true},
		"ipapi.co":                        {parseIPAPICo, `{"country_code":"RU","country_name":"Russia","city":"Moscow","org":"SERV.HOST"}`, LeakGeoSource{Country: "ru", CountryName: "Russia", City: "Moscow", Org: "SERV.HOST"}, false},
		"ipapi.co, rate-limited":          {parseIPAPICo, `{"error":true,"reason":"RateLimited"}`, LeakGeoSource{}, true},
		"ipapi.co, a reserved range":      {parseIPAPICo, `{"country_code":"XX"}`, LeakGeoSource{}, true},
	} {
		got, err := safeParse(c.parse, []byte(c.body))
		if (err != nil) != c.fails || got != c.want {
			t.Errorf("%s: %+v, %v; want %+v (fails %v)", name, got, err, c.want, c.fails)
		}
	}
	// A parser that falls over is a failed service, not a crash.
	if _, err := safeParse(func([]byte) (LeakGeoSource, error) { var m map[string]int; m["x"] = 1; return LeakGeoSource{}, nil }, nil); err == nil {
		t.Error("a panicking parser did not fail")
	}
}

func src(name, cc, city, org string) LeakGeoSource {
	return LeakGeoSource{Name: name, OK: cc != "", Country: cc, CountryName: "Name of " + cc, City: city, Org: org}
}

func TestConsensusGeo(t *testing.T) {
	bash := func(cc string) LeakGeoSource { return src("bash.ws", cc, "", "Hosting Industry") }
	for name, c := range map[string]struct {
		list         []LeakGeoSource
		country      string
		votes, total int
		tie          bool
		city, org    string
		groups       string // "ru:ipwho.is,ipinfo.io ua:bash.ws"
	}{
		"all agree": {
			list:    []LeakGeoSource{bash("ru"), src("a", "ru", "Moscow", "X"), src("b", "ru", "Moscow", "X"), src("c", "ru", "Moskva", "Y")},
			country: "ru", votes: 4, total: 4, city: "Moscow", org: "X", groups: "ru:bash.ws,a,b,c",
		},
		"a majority": {
			list:    []LeakGeoSource{bash("ua"), src("a", "ru", "Moscow", "X"), src("b", "ru", "Moscow", "X"), src("c", "ru", "", ""), src("d", "ua", "Kyiv", "Z")},
			country: "ru", votes: 3, total: 5, city: "Moscow", org: "X", groups: "ru:a,b,c ua:bash.ws,d",
		},
		"a tie goes to bash.ws": {
			list:    []LeakGeoSource{bash("ua"), src("a", "ru", "Moscow", "X"), src("b", "ua", "Kyiv", "Z"), src("c", "ru", "Moscow", "X")},
			country: "ua", votes: 2, total: 4, tie: true, city: "Kyiv", org: "Hosting Industry", groups: "ua:bash.ws,b ru:a,c",
		},
		"a tie without bash.ws goes to the first in the table": {
			list:    []LeakGeoSource{{Name: "bash.ws"}, src("a", "de", "Berlin", "X"), src("b", "ru", "Moscow", "Y")},
			country: "de", votes: 1, total: 2, tie: true, city: "Berlin", org: "X", groups: "de:a ru:b",
		},
		"failed ones do not vote": {
			list:    []LeakGeoSource{bash("ua"), {Name: "a"}, {Name: "b"}, src("c", "ua", "Kyiv", "Z")},
			country: "ua", votes: 2, total: 2, city: "Kyiv", org: "Hosting Industry", groups: "ua:bash.ws,c",
		},
		"nobody": {list: []LeakGeoSource{{Name: "bash.ws"}, {Name: "a"}}},
	} {
		g := consensusGeo(c.list)
		if c.country == "" {
			if g != nil {
				t.Errorf("%s: %+v", name, g)
			}
			continue
		}
		var groups []string
		for _, x := range g.Groups {
			groups = append(groups, x.Country+":"+strings.Join(x.Sources, ","))
		}
		if g.Country != c.country || g.Votes != c.votes || g.Total != c.total || g.Tie != c.tie || g.City != c.city || g.Org != c.org || strings.Join(groups, " ") != c.groups || len(g.Sources) != len(c.list) {
			t.Errorf("%s: %+v, groups %v", name, g, groups)
		}
	}
}

// geoMux serves /1/<ip> ... /5/<ip> from handlers h[1..5]; a missing one 404s.
func geoMux(t *testing.T, h map[int]http.HandlerFunc) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		var n int
		fmt.Sscanf(r.URL.Path, "/%d/", &n)
		if h[n] == nil {
			http.NotFound(w, r)
			return
		}
		h[n](w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), seen...) }
}

func exitOf(cc string) *LeakResult {
	return &LeakResult{Exit: &LeakHost{IP: "203.0.113.5", Country: cc, CountryName: "Name of " + cc, Org: "AS64500 Hosting"}}
}

func directTable(srv *httptest.Server) []exitGeoSource {
	return geoTable(func(i int) string { return fmt.Sprintf("%s/%d", srv.URL, i) })
}

func TestExitGeo(t *testing.T) {
	limit := status(http.StatusTooManyRequests, `{"message":"limit"}`)
	for name, c := range map[string]struct {
		bash    string
		h       map[int]http.HandlerFunc
		country string // "" for none
		votes   int
		total   int
		failed  []string
	}{
		"all agree": {
			bash: "ru", country: "ru", votes: 6, total: 6,
			h: map[int]http.HandlerFunc{1: reply(ipwhoAns("ru", "Moscow", "X")), 2: reply(ipinfoAns("ru", "Moscow", "X")), 3: reply(geojsAns("ru", "Moscow", "X")), 4: reply(freeAns("ru", "Moscow", "X")), 5: reply(ipapiAns("ru", "Moscow", "X"))},
		},
		"a majority against bash.ws": {
			bash: "ua", country: "ru", votes: 4, total: 6,
			h: map[int]http.HandlerFunc{1: reply(ipwhoAns("ru", "Moscow", "X")), 2: reply(ipinfoAns("ru", "Moscow", "X")), 3: reply(geojsAns("ua", "Kyiv", "Z")), 4: reply(freeAns("ru", "", "")), 5: reply(ipapiAns("ru", "Moscow", "X"))},
		},
		"a tie": {
			bash: "ua", country: "ua", votes: 3, total: 6,
			h: map[int]http.HandlerFunc{1: reply(ipwhoAns("ru", "Moscow", "X")), 2: reply(ipinfoAns("ua", "Kyiv", "Z")), 3: reply(geojsAns("ru", "Moscow", "X")), 4: reply(freeAns("ua", "Kyiv", "Z")), 5: reply(ipapiAns("ru", "Moscow", "X"))},
		},
		"some fail in their own ways": {
			bash: "ua", country: "ru", votes: 2, total: 3, failed: []string{"ipinfo.io", "geojs.io", "ipapi.co"},
			h: map[int]http.HandlerFunc{1: reply(ipwhoAns("ru", "Moscow", "X")), 2: limit, 3: reply(`<html>nope</html>`), 4: reply(freeAns("ru", "Moscow", "X")), 5: reply(`{"error":true,"reason":"RateLimited"}`)},
		},
		"all fail: bash.ws alone": {
			bash: "ua", country: "",
			h: map[int]http.HandlerFunc{1: limit, 2: limit, 3: status(http.StatusBadGateway, ""), 4: reply(`{}`), 5: reply(`[]`)},
		},
		"all are gone": {bash: "ua", country: ""},
	} {
		srv, seen := geoMux(t, c.h)
		s := &Service{cfg: Config{leakGeo: directTable(srv)}}
		g := s.exitGeo(context.Background(), srv.Client(), exitOf(c.bash))
		// Every service is asked, once, about the exit address only.
		if len(seen()) != 5 {
			t.Errorf("%s: asked %v", name, seen())
		}
		for _, p := range seen() {
			if !strings.HasSuffix(p, "/203.0.113.5") {
				t.Errorf("%s: asked %s", name, p)
			}
		}
		if c.country == "" {
			if g != nil {
				t.Errorf("%s: geo = %+v", name, g)
			}
			continue
		}
		if g == nil || g.Country != c.country || g.Votes != c.votes || g.Total != c.total {
			t.Errorf("%s: geo = %+v", name, g)
			continue
		}
		var failed []string
		for _, x := range g.Sources {
			if !x.OK {
				failed = append(failed, x.Name)
			}
		}
		if strings.Join(failed, ",") != strings.Join(c.failed, ",") || g.Sources[0].Name != "bash.ws" || len(g.Sources) != 6 {
			t.Errorf("%s: failed %v, sources %+v", name, failed, g.Sources)
		}
	}
}

// The services are asked at once: none answers before all have asked.
func TestExitGeoAsksAtOnce(t *testing.T) {
	var mu sync.Mutex
	arrived := 0
	release := make(chan struct{})
	gate := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrived++
		if arrived == 5 {
			close(release)
		}
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		fmt.Fprint(w, ipapiAns("ru", "Moscow", "X"))
	}
	srv, _ := geoMux(t, map[int]http.HandlerFunc{1: gate, 2: gate, 3: gate, 4: gate, 5: gate})
	s := &Service{cfg: Config{leakGeo: directTable(srv)}}
	// All five use the ipapi.co shape here.
	for i := range s.cfg.leakGeo {
		s.cfg.leakGeo[i].Parse = parseIPAPICo
	}
	start := time.Now()
	g := s.exitGeo(context.Background(), srv.Client(), exitOf("ru"))
	if g == nil || g.Votes != 6 || time.Since(start) > time.Second {
		t.Errorf("geo = %+v after %v (one after the other?)", g, time.Since(start))
	}
}

// A service that does not answer in time is given up on after the timeout,
// without holding the others up.
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
	srv, _ := geoMux(t, map[int]http.HandlerFunc{1: slow, 2: reply(ipinfoAns("ru", "Moscow", "X")), 3: slow})
	s := &Service{cfg: Config{leakGeo: directTable(srv)}}
	start := time.Now()
	g := s.exitGeo(context.Background(), srv.Client(), exitOf("ua"))
	if g == nil || g.Total != 2 || !g.Tie || time.Since(start) > 2*time.Second {
		t.Fatalf("geo = %+v after %v", g, time.Since(start))
	}
	if g.Sources[1].Name != "ipwho.is" || g.Sources[1].OK || !g.Sources[2].OK || g.Sources[3].OK {
		t.Errorf("sources = %+v", g.Sources)
	}
}

// Only a public address of the VPN's goes out.
func TestExitGeoOnlyAPublicAddress(t *testing.T) {
	srv, seen := geoMux(t, map[int]http.HandlerFunc{1: reply(ipwhoAns("ru", "Moscow", "X"))})
	s := &Service{cfg: Config{leakGeo: directTable(srv)}}
	for _, ip := range []string{"", "not an address", "192.168.1.7", "10.1.2.3", "127.0.0.1", "169.254.1.1", "fd00::1", "::", "224.0.0.1"} {
		r := exitOf("ua")
		r.Exit.IP = ip
		if g := s.exitGeo(context.Background(), srv.Client(), r); g != nil {
			t.Errorf("%q was looked up: %+v", ip, g)
		}
	}
	if s.exitGeo(context.Background(), srv.Client(), &LeakResult{}) != nil {
		t.Error("no exit, a lookup")
	}
	if len(seen()) != 0 {
		t.Errorf("asked %v", seen())
	}
	r := exitOf("ua")
	r.Exit.IP = "2001:db8::5"
	if g := s.exitGeo(context.Background(), srv.Client(), r); g == nil || len(seen()) != 5 {
		t.Errorf("an IPv6 address: %+v, %v", g, seen())
	}
}

// The real table: free, HTTPS, no key, the address and nothing else, each
// service once.
func TestLeakGeoSources(t *testing.T) {
	if len(leakGeoSources) < 4 {
		t.Fatalf("%d services", len(leakGeoSources))
	}
	names := map[string]bool{leakGeoBash: true}
	for _, s := range leakGeoSources {
		low := strings.ToLower(s.URL)
		if !strings.HasPrefix(s.URL, "https://") || strings.Count(s.URL, "%s") != 1 || strings.Contains(low, "key") || strings.Contains(low, "token") || s.Parse == nil || names[s.Name] {
			t.Errorf("%s: %s", s.Name, s.URL)
		}
		names[s.Name] = true
	}
	if got := fmt.Sprintf(leakGeoSources[0].URL, "203.0.113.5"); got != "https://ipwho.is/203.0.113.5?lang=ru" {
		t.Errorf("first source: %s", got)
	}
}

// geoHosts maps the handlers to geo1.test ... geo5.test; nil ones are left
// unserved.
func geoHosts(h ...http.HandlerFunc) map[string]http.HandlerFunc {
	out := map[string]http.HandlerFunc{}
	for i, f := range h {
		if f != nil {
			out[fmt.Sprintf("geo%d.test", i+1)] = f
		}
	}
	return out
}

func throughCore() []exitGeoSource {
	return geoTable(func(i int) string { return fmt.Sprintf("http://geo%d.test", i) })
}

func noLookup(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("198.18.0.7")}, nil
}

// The services are reached through the core, like bash.ws: geoN.test
// resolves nowhere else, so a request that went around the core fails.
// bash.ws says Germany for the exit; only the exit address is asked about.
func TestLeakTestGeolocationThroughTheCore(t *testing.T) {
	limit := status(http.StatusTooManyRequests, "")
	for name, c := range map[string]struct {
		geo   map[string]http.HandlerFunc
		want  string // "country votes/total", "" for none
		split string // groups, "" to skip
	}{
		"all agree": {
			geo:  geoHosts(reply(ipwhoAns("de", "Berlin", "X")), reply(ipinfoAns("de", "Berlin", "X")), reply(geojsAns("de", "Berlin", "X")), reply(freeAns("de", "Berlin", "X")), reply(ipapiAns("de", "Berlin", "X"))),
			want: "de 6/6",
		},
		"a majority against bash.ws": {
			geo:   geoHosts(reply(ipwhoAns("ru", "Moscow", "SERV")), reply(ipinfoAns("ru", "Moscow", "SERV")), reply(geojsAns("de", "Berlin", "X")), reply(freeAns("ru", "Moscow", "SERV")), limit),
			want:  "ru 3/5",
			split: "ru:ipwho.is,ipinfo.io,freeipapi de:bash.ws,geojs.io",
		},
		"a tie": {
			geo:   geoHosts(reply(ipwhoAns("ru", "Moscow", "SERV")), reply(ipinfoAns("ru", "Moscow", "SERV")), reply(geojsAns("de", "Berlin", "X")), limit, limit),
			want:  "de 2/4",
			split: "de:bash.ws,geojs.io ru:ipwho.is,ipinfo.io",
		},
		"some fail": {
			geo:  geoHosts(limit, reply(ipinfoAns("de", "Berlin", "X")), reply("garbage"), nil, nil),
			want: "de 2/2",
		},
		"all fail": {
			geo:  geoHosts(limit, limit, limit, limit, limit),
			want: "",
		},
	} {
		h, b := leakGeoHarness(t, noLookup, c.geo, throughCore(), "198.51.100.20")
		res, err := h.svc.LeakTest(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Exit == nil || res.Exit.Country != "de" {
			t.Errorf("%s: exit %+v", name, res.Exit)
		}
		got := ""
		var groups []string
		if g := res.ExitGeo; g != nil {
			got = fmt.Sprintf("%s %d/%d", g.Country, g.Votes, g.Total)
			for _, x := range g.Groups {
				groups = append(groups, x.Country+":"+strings.Join(x.Sources, ","))
			}
		}
		if got != c.want || (c.split != "" && strings.Join(groups, " ") != c.split) {
			t.Errorf("%s: geo %q groups %v, want %q %q", name, got, groups, c.want, c.split)
		}
		// Not a word to the services about anything but the exit address,
		// and only those that are served are heard (the rest 404 unheard).
		for _, q := range b.geoRequests() {
			if !strings.HasSuffix(q, "/"+vpnExit) || strings.Contains(q, "198.51.100.20") {
				t.Errorf("%s: asked %s", name, q)
			}
		}
		if len(b.geoRequests()) != len(c.geo) {
			t.Errorf("%s: asked %v", name, b.geoRequests())
		}
		if res.Verdict != leakOK {
			t.Errorf("%s: verdict %q", name, res.Verdict)
		}
	}
}

// The verdict compares countries with the exit's as the services agree on
// it: the ISP's resolver answering from Russia is the ISP's when the VPN
// server is in Germany, and no leak when it is in Russia after all.
func TestLeakTestVerdictUsesTheConsensus(t *testing.T) {
	var b *fakeBash
	isp := func(_ context.Context, host string) ([]netip.Addr, error) {
		b.mu.Lock()
		b.lookedUp(host, ispResolver)
		b.mu.Unlock()
		return []netip.Addr{netip.MustParseAddr("192.0.2.80")}, nil
	}
	var h *harness
	h, b = leakGeoHarness(t, isp, geoHosts(reply(ipwhoAns("de", "Berlin", "X")), reply(ipinfoAns("de", "Berlin", "X")), reply(geojsAns("de", "Berlin", "X"))), throughCore(), "198.51.100.20")
	res, err := h.svc.LeakTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != leakFound || res.ExitGeo.Country != "de" {
		t.Errorf("exit in Germany: verdict %q, geo %+v", res.Verdict, res.ExitGeo)
	}

	h, b = leakGeoHarness(t, isp, geoHosts(reply(ipwhoAns("ru", "Moscow", "X")), reply(ipinfoAns("ru", "Moscow", "X")), reply(geojsAns("ru", "Moscow", "X")), reply(freeAns("ru", "Moscow", "X"))), throughCore(), "198.51.100.20")
	if res, err = h.svc.LeakTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res.Verdict != leakOK || res.ExitGeo.Country != "ru" || res.Exit.Country != "de" {
		t.Errorf("exit in Russia: verdict %q, geo %+v, exit %+v", res.Verdict, res.ExitGeo, res.Exit)
	}
	for _, d := range res.DNS {
		if d.IP == ispResolver && d.Role != leakRoleVPN {
			t.Errorf("the resolver is %q", d.Role)
		}
	}
}

// The device's own address never goes to the services: not when the test
// went around the VPN, and not when nothing vouches that it went through.
func TestLeakTestGeolocationNeverTheHome(t *testing.T) {
	geo := geoHosts(reply(ipwhoAns("ru", "Moscow", "X")), reply(ipinfoAns("ru", "Moscow", "X")), reply(geojsAns("ru", "Moscow", "X")), reply(freeAns("ru", "Moscow", "X")), reply(ipapiAns("ru", "Moscow", "X")))

	// bash.ws saw the device's address: around the VPN.
	h, b := leakGeoHarness(t, noLookup, geo, throughCore(), vpnExit)
	res, err := h.svc.LeakTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != leakBypass || res.ExitGeo != nil || len(b.geoRequests()) != 0 {
		t.Errorf("bypass: verdict %q, geo %+v, asked %v", res.Verdict, res.ExitGeo, b.geoRequests())
	}

	// The device's address is unknown and the exit is not the server's.
	h, b = leakGeoHarness(t, noLookup, geo, throughCore(), "198.51.100.20")
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
}

// The result carries the vote as exit_geo, and leaves it out when there is
// none.
func TestLeakGeoJSON(t *testing.T) {
	r := LeakResult{DNS: []LeakHost{}, ExitGeo: consensusGeo([]LeakGeoSource{src("bash.ws", "ua", "", ""), src("ipwho.is", "ru", "Moscow", "X"), {Name: "ipinfo.io"}})}
	b, _ := json.Marshal(r)
	for _, want := range []string{`"exit_geo":{"country":"ua"`, `"votes":1`, `"total":2`, `"tie":true`, `"groups":[{"country":"ua"`, `"sources":["bash.ws"]`, `{"name":"ipinfo.io","ok":false}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %s", b, want)
		}
	}
	r.ExitGeo = nil
	if b, _ = json.Marshal(r); strings.Contains(string(b), "exit_geo") {
		t.Errorf("%s", b)
	}
}
