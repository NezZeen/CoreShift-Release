package service

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/ruleset"
	"coreshift/engine/internal/store"
)

func storeRule(match string) store.Rule { return store.Rule{Match: match, Action: store.RuleDirect} }

const lists = "https://lists.example/geosite.dat"

// geoAPI is the API over a store, whose source of categories is lists,
// served by the function; the listing of SagerNet's branch (for geoip) too.
func geoAPI(t *testing.T, serve func(ctx context.Context, link string) ([]byte, error)) (*harness, func(method, path string, body any, out any) int) {
	h, srv := newStoreAPI(t, func(c *Config) {
		c.fetchGeo = func(ctx context.Context, link string, _ *url.URL, _ int64) ([]byte, error) { return serve(ctx, link) }
	})
	return h, func(method, path string, body any, out any) int { return callJSON(t, srv, method, path, body, out) }
}

// useSource saves the source of categories and the rules.
func useSource(t *testing.T, do func(method, path string, body any, out any) int, geosite string, rules ...map[string]string) {
	t.Helper()
	var set map[string]any
	if code := do("GET", "/v1/settings", nil, &set); code != 200 {
		t.Fatalf("settings: %d", code)
	}
	routing := set["routing"].(map[string]any)
	routing["geo"] = map[string]any{"source": "custom", "geosite_url": geosite, "geoip_url": "", "presets": false}
	routing["rules"] = rules
	if code := do("PUT", "/v1/settings", set, nil); code != 200 {
		t.Fatalf("put settings: %d", code)
	}
}

func geoIPListing(link string) ([]byte, bool) {
	if link == ruleset.SagerNetGeoIPIndex {
		return []byte(`{"tree":[{"path":"geoip-ru.srs","type":"blob"},{"path":"geoip-ua.srs","type":"blob"},{"path":"geoip-private.srs","type":"blob"}]}`), true
	}
	return nil, false
}

// The base is shown loading, with how far it is, then loaded and when; the
// categories endpoint says what a name comes to; rules that wait for the
// base say so.
func TestGeoStatusAndCategories(t *testing.T) {
	dat := geositeDat(map[string][]string{"category-ru": {"yandex.ru"}, "category-ua": {"ukr.net"}, "ua": {"gov.ua"}, "youtube": {"youtube.com"}})
	release := make(chan struct{})
	_, do := geoAPI(t, func(ctx context.Context, link string) ([]byte, error) {
		if b, ok := geoIPListing(link); ok {
			return b, nil
		}
		if link == lists {
			if p, _ := ctx.Value(progressKey{}).(*progress); p != nil {
				p.total.Store(200)
				p.got.Store(50)
			}
			select {
			case <-release:
				return dat, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return nil, &httpStatusError{404, "404 Not Found"}
	})
	var st GeoStatus
	do("GET", "/v1/geo/status", nil, &st)
	if len(st.Bases) != 2 || st.Bases[0].State != BaseIdle || st.Bases[0].Origin != "sagernet" {
		t.Fatalf("status of a fresh install: %+v", st)
	}
	useSource(t, do, lists, map[string]string{"match": "geosite:ru", "action": "direct"}, map[string]string{"match": "example.org", "action": "block"})

	// Under way: 50 of 200 bytes.
	waitFor(t, "loading", func() bool {
		st = GeoStatus{}
		do("GET", "/v1/geo/status", nil, &st)
		return st.Bases[0].State == BaseLoading && st.Bases[0].Bytes == 50
	})
	if b := st.Bases[0]; b.Percent != 25 || b.Total != 200 || b.Origin != "custom" || b.Kind != "geosite" {
		t.Errorf("loading: %+v", b)
	}
	if len(st.Rules) != 1 || st.Rules[0].Match != "geosite:ru" || st.Rules[0].State != RuleWaiting {
		t.Errorf("rules while loading: %+v", st.Rules)
	}
	// The editor asks meanwhile: not loaded, not an answer about the name.
	var c GeoCategories
	if code := do("GET", "/v1/geo/categories?kind=geosite&q=ru", nil, &c); code != 200 || c.Loaded || c.State != BaseLoading || c.Resolved != "" || len(c.Matches) != 0 {
		t.Errorf("while loading: %d %+v", code, c)
	}

	close(release)
	waitFor(t, "loaded", func() bool {
		st = GeoStatus{}
		do("GET", "/v1/geo/status", nil, &st)
		return st.Bases[0].State == BaseLoaded
	})
	if b := st.Bases[0]; b.Count != 4 || time.Since(b.LoadedAt) > time.Minute || b.Error != "" || b.Percent != 0 {
		t.Errorf("loaded: %+v", b)
	}
	if len(st.Rules) != 1 || st.Rules[0].State != RuleResolved || st.Rules[0].Name != "category-ru" {
		t.Errorf("rules after: %+v", st.Rules)
	}

	for _, q := range []struct {
		q                     string
		exact                 bool
		resolved              string
		ambiguous, suggestion []string
		matches               []string
	}{
		{"youtube", true, "youtube", nil, nil, []string{"youtube"}},
		{"ru", false, "category-ru", nil, nil, []string{"category-ru"}},
		{"U_a", false, "", []string{"category-ua", "ua"}, nil, []string{"ua", "category-ua"}},
		{"ua", true, "ua", nil, nil, []string{"ua", "category-ua"}},
		{"uua", false, "", nil, []string{"category-ua", "ua"}, nil},
		{"nothing", false, "", nil, nil, nil},
		{"", false, "", nil, nil, nil},
	} {
		var c GeoCategories
		if code := do("GET", "/v1/geo/categories?kind=geosite&q="+url.QueryEscape(q.q), nil, &c); code != 200 {
			t.Fatalf("%q: %d", q.q, code)
		}
		same := func(a, b []string) bool { return slices.Equal(append([]string{}, a...), append([]string{}, b...)) }
		if !c.Loaded || !c.Listed || c.Exact != q.exact || c.Resolved != q.resolved || !same(c.Ambiguous, q.ambiguous) || !same(c.Suggestions, q.suggestion) || !same(c.Matches, q.matches) {
			t.Errorf("%q: %+v", q.q, c)
		}
	}
	// geoip comes from SagerNet's listing, loaded on request.
	var ip GeoCategories
	ip = GeoCategories{}
	do("GET", "/v1/geo/categories?kind=geoip&q=UA", nil, &ip)
	waitFor(t, "geoip listing", func() bool {
		do("GET", "/v1/geo/categories?kind=geoip&q=UA", nil, &ip)
		return ip.Loaded
	})
	if ip.Resolved != "ua" || !ip.Exact || ip.Origin != "sagernet" || ip.Count != 3 {
		t.Errorf("geoip: %+v", ip)
	}

	// Bad requests.
	if code := do("GET", "/v1/geo/categories?kind=domain&q=x", nil, nil); code != http.StatusBadRequest {
		t.Errorf("kind: %d", code)
	}
	if code := do("GET", "/v1/geo/categories?kind=geosite&q="+strings.Repeat("a", 200), nil, nil); code != http.StatusBadRequest {
		t.Errorf("long name: %d", code)
	}
	if code := do("POST", "/v1/geo/reload", map[string]string{"kind": "nope"}, nil); code != http.StatusBadRequest {
		t.Errorf("reload kind: %d", code)
	}
}

// A base that did not download says why and is tried again on request, at
// once, not after the pause that follows a failure.
func TestGeoStatusErrorAndRetry(t *testing.T) {
	dat := geositeDat(map[string][]string{"category-ru": {"yandex.ru"}})
	var up atomic.Bool
	var hits atomic.Int32
	_, do := geoAPI(t, func(ctx context.Context, link string) ([]byte, error) {
		if b, ok := geoIPListing(link); ok {
			return b, nil
		}
		hits.Add(1)
		if !up.Load() {
			return nil, &net.DNSError{Err: "no such host", Name: "lists.example"}
		}
		return dat, nil
	})
	useSource(t, do, lists, map[string]string{"match": "geosite:ru", "action": "direct"})
	var st GeoStatus
	waitFor(t, "error", func() bool {
		st = GeoStatus{}
		do("GET", "/v1/geo/status", nil, &st)
		return st.Bases[0].State == BaseError
	})
	if b := st.Bases[0]; !strings.Contains(b.Error, "no such host") || strings.Contains(b.Error, "\n") {
		t.Errorf("error: %q", b.Error)
	}
	if len(st.Rules) != 1 || st.Rules[0].State != RuleError {
		t.Errorf("rules: %+v", st.Rules)
	}
	// Asking again at once does not hammer the source...
	n := hits.Load()
	do("GET", "/v1/geo/categories?kind=geosite&q=ru", nil, nil)
	time.Sleep(50 * time.Millisecond)
	if hits.Load() != n {
		t.Errorf("tried again at once: %d, %d", n, hits.Load())
	}
	// ...but "Повторить" does.
	up.Store(true)
	if code := do("POST", "/v1/geo/reload", map[string]string{"kind": "geosite"}, &st); code != 200 {
		t.Fatalf("reload: %d", code)
	}
	waitFor(t, "loaded after a retry", func() bool {
		st = GeoStatus{}
		do("GET", "/v1/geo/status", nil, &st)
		return st.Bases[0].State == BaseLoaded
	})
	if st.Rules[0].State != RuleResolved {
		t.Errorf("rules: %+v", st.Rules)
	}
}

// A link that lists nothing has no base to show or to wait for.
func TestGeoStatusNoListing(t *testing.T) {
	_, do := geoAPI(t, func(ctx context.Context, link string) ([]byte, error) {
		if b, ok := geoIPListing(link); ok {
			return b, nil
		}
		return nil, &httpStatusError{404, "404 Not Found"}
	})
	useSource(t, do, "https://lists.example/geosite-{name}.srs", map[string]string{"match": "geosite:anything", "action": "proxy"})
	var st GeoStatus
	st = GeoStatus{}
	do("GET", "/v1/geo/status", nil, &st)
	if st.Bases[0].State != BaseNone || len(st.Rules) != 0 {
		t.Errorf("status %+v", st)
	}
	var c GeoCategories
	if code := do("GET", "/v1/geo/categories?kind=geosite&q=anything", nil, &c); code != 200 || c.Listed || c.Loaded || c.State != BaseNone {
		t.Errorf("categories: %d %+v", code, c)
	}
}

// The endpoints are behind the token and the host check, as the rest.
func TestGeoEndpointsAreGuarded(t *testing.T) {
	_, srv := newStoreAPI(t, nil)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	for _, route := range []struct{ method, path string }{
		{"GET", "/v1/geo/status"},
		{"GET", "/v1/geo/categories?kind=geosite&q=ru"},
		{"POST", "/v1/geo/reload"},
	} {
		if code, _ := call(t, srv, route.method, route.path, "", func(r *http.Request) { r.Header.Del("Authorization") }); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", route.method, route.path, code)
		}
		if code, _ := call(t, srv, route.method, route.path, "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }); code != http.StatusUnauthorized {
			t.Errorf("%s %s with a wrong token: %d", route.method, route.path, code)
		}
		if code, _ := call(t, srv, route.method, route.path, "", func(r *http.Request) { r.Host = "evil.example:" + port }); code != http.StatusForbidden {
			t.Errorf("%s %s from a foreign host: %d", route.method, route.path, code)
		}
		if code, _ := call(t, srv, route.method, route.path, ""); code != http.StatusOK {
			t.Errorf("%s %s: %d", route.method, route.path, code)
		}
	}
}

// The words of the bases' journal lines never carry the link.
func TestGeoLinesKeepTheLinkOut(t *testing.T) {
	rig := newRuleSetsRig(t, time.Now())
	var lines []string
	rig.publish = func(e Event) { lines = append(lines, e.Error) }
	rig.skipped(storeRule("geosite:x"), "https://lists.example/geosite.dat?token=SECRET", context.DeadlineExceeded)
	rig.skipped(storeRule("geosite:x"), "https://lists.example/{name}.srs?token=SECRET", &httpStatusError{404, "404 Not Found"})
	rig.resolved(storeRule("geosite:x"), "geosite", "xx", "https://lists.example/geosite.dat?token=SECRET")
	for _, l := range lines {
		if strings.Contains(l, "SECRET") || strings.Contains(l, "lists.example") {
			t.Errorf("the link in %q", l)
		}
	}
	if len(lines) != 3 {
		t.Errorf("lines %q", lines)
	}
}
