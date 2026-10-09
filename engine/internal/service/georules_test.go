package service

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"google.golang.org/protobuf/encoding/protowire"

	"coreshift/engine/internal/ruleset"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/tunlayer"
)

// namesSet and addrSet are rule sets of names (and below), of networks.
func namesSet(t *testing.T, names ...string) []byte {
	return compileSet(t, option.DefaultHeadlessRule{DomainSuffix: names})
}

func addrSet(t *testing.T, cidrs ...string) []byte {
	return compileSet(t, option.DefaultHeadlessRule{IPCIDR: cidrs})
}

func compileSet(t *testing.T, r option.DefaultHeadlessRule) []byte {
	t.Helper()
	var buf bytes.Buffer
	rs := option.PlainRuleSet{Rules: []option.HeadlessRule{{Type: C.RuleTypeDefault, DefaultOptions: r}}}
	if err := srs.Write(&buf, rs, C.RuleSetVersion2); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// geositeDat is a v2ray list with categories of domains.
func geositeDat(cats map[string][]string) []byte {
	var dat []byte
	for code, names := range cats {
		entry := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), strings.ToUpper(code))
		for _, n := range names {
			d := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 2)
			d = protowire.AppendString(protowire.AppendTag(d, 2, protowire.BytesType), n)
			entry = protowire.AppendBytes(protowire.AppendTag(entry, 2, protowire.BytesType), d)
		}
		dat = protowire.AppendBytes(protowire.AppendTag(dat, 1, protowire.BytesType), entry)
	}
	return dat
}

// source is a local https server of rule sets (/srs/<file>) and a v2ray
// list (/geosite.dat), counting requests by path.
type source struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	hits  map[string]int
}

func newSource(t *testing.T) *source {
	src := &source{files: map[string][]byte{}, hits: map[string]int{}}
	src.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		src.mu.Lock()
		src.hits[r.URL.Path]++
		b, ok := src.files[r.URL.Path]
		src.mu.Unlock()
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://"+r.Host+"/srs/geosite-youtube.srs", http.StatusFound)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(src.Close)
	return src
}

func (s *source) set(path string, b []byte) {
	s.mu.Lock()
	s.files[path] = b
	s.mu.Unlock()
}

func (s *source) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// geoRig is a ruleSets that downloads from src for real, over its TLS.
func geoRig(t *testing.T, src *source) *ruleSetsRig {
	rig := newRuleSetsRig(t, time.Now().Add(-time.Hour))
	rig.transport = func(*url.URL) *http.Transport {
		return src.Client().Transport.(*http.Transport).Clone()
	}
	var mu sync.Mutex
	rig.publish = func(e Event) {
		mu.Lock()
		rig.events = append(rig.events, e)
		mu.Unlock()
	}
	return rig
}

func TestCategoryFromTemplate(t *testing.T) {
	src := newSource(t)
	src.set("/srs/geosite-youtube.srs", namesSet(t, "youtube.com", "googlevideo.com"))
	src.set("/srs/geoip-ru.srs", addrSet(t, "77.88.0.0/18"))
	src.set("/srs/geosite-junk.srs", []byte("<html>not a rule set</html>"))
	src.set("/srs/geosite-addresses.srs", addrSet(t, "10.0.0.0/8"))
	rig := geoRig(t, src)
	link := src.URL + "/srs/geosite-{name}.srs"
	ctx := context.Background()

	path, err := rig.category(ctx, link, false, "youtube", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); ruleset.Validate(b, false) != nil || !strings.HasPrefix(path, filepath.Join(rig.dir, "geo")) {
		t.Errorf("not cached under geo: %s", path)
	}
	// Cached: not downloaded again.
	if _, err := rig.category(ctx, link, false, "youtube", nil); err != nil || src.count("/srs/geosite-youtube.srs") != 1 {
		t.Errorf("downloaded again: %v, %d", err, src.count("/srs/geosite-youtube.srs"))
	}
	if _, err := rig.category(ctx, src.URL+"/srs/geoip-{name}.srs", true, "ru", nil); err != nil {
		t.Error(err)
	}
	// What is not a rule set of names is not taken.
	for _, name := range []string{"junk", "addresses", "missing"} {
		if _, err := rig.category(ctx, link, false, name, nil); err == nil {
			t.Errorf("%s: taken", name)
		}
		if _, err := os.Stat(filepath.Join(rig.dir, "geo", linkKey(link), "geosite-"+name+".srs")); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	// A failure is not tried again at once.
	n := src.count("/srs/geosite-missing.srs")
	if _, err := rig.category(ctx, link, false, "missing", nil); err == nil || src.count("/srs/geosite-missing.srs") != n {
		t.Errorf("tried again at once: %v", err)
	}
	// Old: refreshed in the background, the old copy working meanwhile.
	old := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(path, old, old)
	src.set("/srs/geosite-youtube.srs", namesSet(t, "youtube.com", "googlevideo.com", "ytimg.com"))
	if _, err := rig.category(ctx, link, false, "youtube", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for src.count("/srs/geosite-youtube.srs") < 2 || !fresh(path) {
		if time.Now().After(deadline) {
			t.Fatal("an old set was not refreshed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func fresh(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && time.Since(fi.ModTime()) < time.Hour
}

func TestCategoryHTTPSOnlyAndLimited(t *testing.T) {
	src := newSource(t)
	rig := geoRig(t, src)
	ctx := context.Background()
	if _, err := rig.httpsGet(ctx, "http://example.org/x.srs", nil, ruleset.MaxSize); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("plain http: %v", err)
	}
	if _, err := rig.httpsGet(ctx, src.URL+"/redirect", nil, ruleset.MaxSize); err == nil || !strings.Contains(err.Error(), "without https") {
		t.Errorf("redirect to http: %v", err)
	}
	src.set("/big.dat", bytes.Repeat([]byte{1}, 2048))
	if _, err := rig.httpsGet(ctx, src.URL+"/big.dat", nil, 1024); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("over the limit: %v", err)
	}
	// The link, which may carry a token, is not in the error.
	if _, err := rig.httpsGet(ctx, "https://127.0.0.1:1/x.srs?token=secret", nil, ruleset.MaxSize); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error names the link: %v", err)
	}
}

func TestCategoryFromDat(t *testing.T) {
	src := newSource(t)
	src.set("/geosite.dat", geositeDat(map[string][]string{
		"ru-blocked":       {"meduza.io", "linkedin.com"},
		"category-ads-all": {"doubleclick.net"},
	}))
	rig := geoRig(t, src)
	link := src.URL + "/geosite.dat"
	ctx := context.Background()
	for _, name := range []string{"ru-blocked", "category-ads-all", "ru-blocked"} {
		path, err := rig.category(ctx, link, false, name, nil)
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(path); ruleset.Validate(b, false) != nil {
			t.Errorf("%s: not a rule set", name)
		}
	}
	if n := src.count("/geosite.dat"); n != 1 {
		t.Errorf("the list was downloaded %d times", n)
	}
	if _, err := rig.category(ctx, link, false, "youtube", nil); err == nil || !strings.Contains(err.Error(), "no category") {
		t.Errorf("a missing category: %v", err)
	}
	// A newer list: the categories are taken out of it again.
	dat := filepath.Join(rig.dir, "geo", linkKey(link), "geosite.dat")
	b := geositeDat(map[string][]string{"ru-blocked": {"meduza.io", "linkedin.com", "new.example"}})
	os.WriteFile(dat, b, 0o600)
	future := time.Now().Add(time.Minute)
	os.Chtimes(dat, future, future)
	path, err := rig.category(ctx, link, false, "ru-blocked", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, mustFromDat(t, b, "ru-blocked")) {
		t.Error("not taken out of the newer list")
	}
	// A damaged list is not taken.
	src.set("/other.dat", []byte("<html>rate limited</html>"))
	if _, err := rig.category(ctx, src.URL+"/other.dat", false, "ru-blocked", nil); err == nil {
		t.Error("a damaged list was taken")
	}
}

func mustFromDat(t *testing.T, dat []byte, name string) []byte {
	t.Helper()
	b, err := ruleset.FromDat(dat, false, name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The user's rules: a category that cannot be had is skipped and said so,
// the rest work; a slow one does not hold connecting up, and comes for the
// next connection.
func TestRoutingUserRules(t *testing.T) {
	src := newSource(t)
	src.set("/srs/geosite-youtube.srs", namesSet(t, "youtube.com"))
	src.set("/srs/geoip-cn.srs", addrSet(t, "1.0.1.0/24"))
	src.set("/srs/geosite-slow.srs", namesSet(t, "slow.example"))
	rig := geoRig(t, src)
	defer func(w time.Duration) { setsWait = w }(setsWait)
	setsWait = 300 * time.Millisecond
	var late atomic.Bool
	rig.late = func() { late.Store(true) }

	o := Options{
		Rules: []store.Rule{
			{Match: "geosite:youtube", Action: store.RuleProxy},
			{Match: "geosite:missing", Action: store.RuleDirect},
			{Match: "bank.example", Action: store.RuleDirect},
			{Match: "geoip:cn", Action: store.RuleBlock},
			{Match: "10.8.0.0/16", Action: store.RuleDirect},
			{Match: "geosite:slow", Action: store.RuleBlock},
		},
		Geo: store.GeoSource{Source: store.GeoCustom, GeositeURL: src.URL + "/srs/geosite-{name}.srs", GeoIPURL: src.URL + "/srs/geoip-{name}.srs"},
	}
	// The slow one answers after connecting stopped waiting.
	slow := make(chan struct{})
	src.Config.Handler = slowFor(src.Config.Handler, "/srs/geosite-slow.srs", slow)

	start := time.Now()
	out := rig.routing(context.Background(), o, nil)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("connecting waited %v", took)
	}
	var got []string
	for _, r := range out.rules {
		switch {
		case r.Set != nil:
			got = append(got, r.Action+" "+r.Set.Tag)
		case len(r.Domains) > 0:
			got = append(got, r.Action+" "+r.Domains[0])
		default:
			got = append(got, r.Action+" "+r.IPs[0].String())
		}
	}
	want := []string{"proxy user-geosite-youtube", "direct bank.example", "block user-geoip-cn", "direct 10.8.0.0/16"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("rules %v, want %v", got, want)
	}
	if !out.rules[2].SetIP || out.rules[0].SetIP {
		t.Error("address sets not marked")
	}
	skipped := map[string]string{}
	for _, e := range rig.events {
		if e.Line == "skipped" {
			skipped[e.Reason] = e.Error
		}
	}
	if !strings.Contains(skipped["geosite:missing"], "не загрузилась") || !strings.Contains(skipped["geosite:slow"], "после переподключения") {
		t.Errorf("skipped: %v", skipped)
	}
	close(slow)
	deadline := time.Now().Add(5 * time.Second)
	for !late.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the late set did not ask for reconnecting")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if out := rig.routing(context.Background(), o, nil); len(out.rules) != 5 {
		t.Errorf("the late set is not used next time: %+v", out.rules)
	}
}

// slowFor holds requests for path until release is closed.
func slowFor(h http.Handler, path string, release chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			<-release
		}
		h.ServeHTTP(w, r)
	})
}

// The presets from another source: what it has replaces the built-in set,
// what it has not leaves the built-in one, and says so. The sets
// CoreShift takes out of runetfreedom's list are never downloaded by the
// service.
func TestRoutingPresetsFromSource(t *testing.T) {
	src := newSource(t)
	src.set("/geosite.dat", geositeDat(map[string][]string{
		"category-ru":      {"yandex.ru", "ya.ru", "vk.com", "mail.ru", "gosuslugi.ru", "ozon.ru", "yandex.net"},
		"category-ads-all": {"doubleclick.net"},
	}))
	rig := geoRig(t, src)
	adsCopy := namesSet(t, "doubleclick.net", "built-in-ads.example")
	rig.baseline = func(tag string) ([]byte, time.Time, bool) {
		if tag == adsSet.Tag {
			return adsCopy, time.Now().Add(-30 * 24 * time.Hour), true
		}
		b, _, ok := ruleset.Baseline(tag)
		return b, time.Now().Add(-time.Hour), ok
	}
	rig.fetch = func(context.Context, string, *url.URL) ([]byte, error) { return nil, errors.New("offline") }
	o := Options{
		BlockAds: true,
		DNS:      DNSSettings{RussiaDirect: true},
		Geo:      store.GeoSource{Source: store.GeoCustom, GeositeURL: src.URL + "/geosite.dat", Presets: true},
	}
	out := rig.routing(context.Background(), o, nil)
	dir := filepath.Join(rig.dir, "geo", linkKey(src.URL+"/geosite.dat"))
	if len(out.domain) != 1 || filepath.Dir(out.domain[0].Path) != dir || out.domain[0].Tag != "geosite-category-ru" {
		t.Errorf("category-ru not from the source: %+v", out.domain)
	}
	if len(out.block) != 1 || filepath.Dir(out.block[0].Path) != dir {
		t.Errorf("the ad set not from the source: %+v", out.block)
	}
	// geoip-ru: the source has no addresses, SagerNet's is the built-in one.
	if len(out.ip) != 1 || filepath.Dir(out.ip[0].Path) != rig.dir {
		t.Errorf("geoip-ru: %+v", out.ip)
	}
	// Google is not in the list: the built-in set, said so.
	if len(out.pinned) != 1 || filepath.Dir(out.pinned[0].Path) != rig.dir {
		t.Errorf("geosite-google: %+v", out.pinned)
	}
	fallback := false
	for _, e := range rig.events {
		fallback = fallback || e.Line == "fallback" && e.Reason == "geosite-google" && strings.Contains(e.Error, "встроенная")
	}
	if !fallback {
		t.Errorf("no word of the built-in set standing in: %+v", rig.events)
	}

	// From SagerNet: the built-in ad set, not refreshed though old.
	o.Geo = store.GeoSource{Source: store.GeoSagerNet}
	rig.fetch = func(context.Context, string, *url.URL) ([]byte, error) {
		t.Error("the ad set was downloaded")
		return nil, errors.New("no")
	}
	out = rig.routing(context.Background(), Options{BlockAds: true}, nil)
	if len(out.block) != 1 || out.block[0].Tag != adsSet.Tag {
		t.Fatalf("ads: %+v", out.block)
	}
	if b, _ := os.ReadFile(out.block[0].Path); !bytes.Equal(b, adsCopy) {
		t.Error("not the built-in ad set")
	}
	time.Sleep(100 * time.Millisecond) // a refresh would have started
	// Not carried by this build: left out, never downloaded.
	rig.baseline = noBaseline
	os.Remove(filepath.Join(rig.dir, adsSet.Tag+".srs"))
	rig.events = nil
	if out := rig.routing(context.Background(), Options{BlockAds: true}, nil); len(out.block) != 0 {
		t.Errorf("ads without a copy: %+v", out.block)
	}
	if len(rig.events) == 0 || !strings.Contains(rig.events[len(rig.events)-1].Error, "реклама не блокируется") {
		t.Errorf("events %+v", rig.events)
	}
}

// A rule whose set cannot be had does not stop connecting.
func TestFailedUserSetStillConnects(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.Rules = []store.Rule{{Match: "geosite:youtube", Action: store.RuleProxy}, {Match: "example.org", Action: store.RuleBlock}}
		c.Geo = store.GeoSource{Source: store.GeoRunetFreedom}
		c.fetchGeo = func(context.Context, string, *url.URL, int64) ([]byte, error) {
			return nil, errors.New("server returned 404 Not Found")
		}
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.tun.mu.Lock()
	o := h.tun.opts
	h.tun.mu.Unlock()
	if len(o.Rules) != 1 || o.Rules[0].Action != tunlayer.ActionBlock || o.Rules[0].Domains[0] != "example.org" {
		t.Errorf("rules = %+v", o.Rules)
	}
	warned := false
	for len(h.events) > 0 {
		if e := <-h.events; e.Kind == "rules" && e.Line == "skipped" && strings.Contains(e.Error, "runetfreedom") {
			warned = true
		}
	}
	if !warned {
		t.Error("no word of the skipped rule")
	}
}
