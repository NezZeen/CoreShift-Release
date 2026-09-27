package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/subscription"
)

// panel is a fake subscription server.
type panel struct {
	mu    sync.Mutex
	body  map[string]string // URL → links
	info  subscription.Info
	fail  error
	calls []string // user agents, in order
}

func (p *panel) fetch(_ context.Context, url, ua string) (subscription.Fetched, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, ua)
	if p.fail != nil {
		return subscription.Fetched{}, p.fail
	}
	body, ok := p.body[url]
	if !ok {
		return subscription.Fetched{}, errors.New("fetch subscription: server returned 404 Not Found")
	}
	res, err := subscription.Parse([]byte(body))
	return subscription.Fetched{Result: res, Info: p.info}, err
}

func (p *panel) set(url, body string) {
	p.mu.Lock()
	p.body[url] = body
	p.mu.Unlock()
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fixture struct {
	path    string
	panel   *panel
	clock   *clock
	changes []Change
	mu      sync.Mutex
}

func (f *fixture) open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(f.path, f.options())
	if err != nil {
		t.Fatal(err)
	}
	f.watch(s)
	return s
}

func (f *fixture) watch(s *Store) {
	s.Watch(func(c Change) {
		f.mu.Lock()
		f.changes = append(f.changes, c)
		f.mu.Unlock()
	})
}

func (f *fixture) options() Options {
	return Options{fetch: f.panel.fetch, now: f.clock.now}
}

func (f *fixture) whats() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.changes {
		out = append(out, c.What)
	}
	return out
}

func newFixture(t *testing.T) *fixture {
	return &fixture{
		path:  filepath.Join(t.TempDir(), "state", "store.json"),
		panel: &panel{body: map[string]string{}},
		clock: &clock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
	}
}

const (
	subURL  = "https://panel.example/sub/SECRET-TOKEN"
	nodeA   = "trojan://pw@203.0.113.5:443?sni=a.example.com#Amsterdam"
	nodeB   = "trojan://pw@203.0.113.6:443?sni=b.example.com#Berlin"
	nodeA2  = "trojan://pw2@203.0.113.50:443?sni=a.example.com#Amsterdam" // same name, new parameters
	pasted  = nodeA + "\n" + nodeB
	badLink = "socks://x@203.0.113.1:1#Unsupported"
)

func TestFreshStoreHasDefaults(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	if got := s.Settings(); !got.TUN || got.Cores.Mode != ModeAuto || len(got.Cores.Priority) != 3 {
		t.Fatalf("defaults = %+v", got)
	}
	if _, err := os.Stat(f.path); !errors.Is(err, os.ErrNotExist) {
		t.Error("opening must not create the file")
	}
	if _, _, ok := s.Selected(); ok {
		t.Error("fresh store has a selection")
	}
}

func TestSettingsAreNormalizedAndPersisted(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	set := s.Settings()
	set.Cores.Priority = []core.Kind{core.SingBox, core.Xray, core.SingBox}
	set.Routing.DirectDomains = []string{" .RU ", "xn--p1ai", "ru", ""}
	set.DNS.Direct = " 192.168.1.1 "
	set.Routing.DirectApps = []string{` C:\Program Files\qBittorrent\qbittorrent.exe `, "QBittorrent.exe", "/opt/steam/steam.sh", ""}
	saved, err := s.SetSettings(set)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved.Cores.Priority, []core.Kind{core.SingBox, core.Xray}) ||
		!slices.Equal(saved.Routing.DirectDomains, []string{"ru", "xn--p1ai"}) || saved.DNS.Direct != "192.168.1.1" ||
		!slices.Equal(saved.Routing.DirectApps, []string{"qbittorrent.exe", "steam.sh"}) {
		t.Fatalf("saved = %+v", saved)
	}
	again := f.open(t).Settings()
	if !slices.Equal(again.Routing.DirectDomains, saved.Routing.DirectDomains) || again.DNS.Direct != "192.168.1.1" {
		t.Errorf("after reopen = %+v", again)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(f.path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %v, %v", fi.Mode(), err)
		}
	}
	if !slices.Equal(f.whats(), []string{"settings"}) {
		t.Errorf("changes = %v", f.whats())
	}
}

func TestInvalidSettingsAreRejected(t *testing.T) {
	s := newFixture(t).open(t)
	set := s.Settings()
	set.Cores.Priority = []core.Kind{"v2ray"}
	set.Cores.HealthIntervalS = 1
	set.DNS.Remote = "ftp://dns.example"
	set.Routing.DirectDomains = []string{"рф", "bad domain"}
	set.Routing.DirectApps = []string{"what?.exe"}
	_, err := s.SetSettings(set)
	if err == nil {
		t.Fatal("accepted invalid settings")
	}
	for _, want := range []string{"cores.priority", "health_interval_s", "dns.remote", "punycode", "bad domain", "what?.exe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
	if got := s.Settings(); got.Cores.HealthIntervalS != 15 {
		t.Errorf("rejected settings were applied: %+v", got)
	}

	set = s.Settings()
	set.Cores.Mode, set.Cores.Manual = ModeManual, ""
	if _, err := s.SetSettings(set); err == nil || !strings.Contains(err.Error(), "cores.manual") {
		t.Errorf("manual without a core: %v", err)
	}
}

func TestAddSubscription(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted+"\n"+badLink)
	f.panel.info = subscription.Info{Title: "My VPN", Total: 100 << 30, UpdateInterval: 6 * time.Hour}
	s := f.open(t)
	set := s.Settings()
	set.Updates.UserAgent = "Custom/1"
	s.SetSettings(set)

	sub, err := s.Add(context.Background(), AddRequest{URL: " " + subURL + " "})
	if err != nil {
		t.Fatal(err)
	}
	if sub.URL != subURL || len(sub.Nodes) != 2 || len(sub.Skipped) != 1 || sub.DisplayName() != "My VPN" ||
		sub.Info.UpdateIntervalHours != 6 || sub.UpdatedAt.IsZero() {
		t.Fatalf("sub = %+v", sub)
	}
	if f.panel.calls[0] != "Custom/1" {
		t.Errorf("user agent = %q", f.panel.calls[0])
	}
	if _, err := s.Add(context.Background(), AddRequest{URL: subURL}); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	_, err = s.Add(context.Background(), AddRequest{URL: "https://panel.example/sub/OTHER-TOKEN"})
	if err == nil || strings.Contains(err.Error(), "OTHER-TOKEN") {
		t.Errorf("failed fetch: %v", err)
	}
	if _, err := s.Add(context.Background(), AddRequest{URL: "panel.example/sub"}); err == nil {
		t.Error("accepted a URL without a scheme")
	}
	if got := len(f.open(t).Subscriptions()); got != 1 {
		t.Errorf("%d subscriptions saved, want 1", got)
	}
}

func TestServersPastedOneByOneShareAList(t *testing.T) {
	s := newFixture(t).open(t)
	ctx := context.Background()
	first, err := s.Add(ctx, AddRequest{Content: nodeA})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Add(ctx, AddRequest{Content: pasted})
	if err != nil || sub.ID != first.ID || len(sub.Nodes) != 2 || sub.Nodes[1].Name != "Berlin" {
		t.Fatalf("second paste: %v %+v", err, sub)
	}
	if _, err := s.Add(ctx, AddRequest{Content: nodeB}); !errors.Is(err, ErrServersExist) {
		t.Errorf("pasting a server again: %v", err)
	}
	// A named list stays a list of its own.
	if named, err := s.Add(ctx, AddRequest{Content: nodeB, Name: "Work"}); err != nil || named.ID == first.ID {
		t.Fatalf("named paste: %v %+v", err, named)
	}
	if subs := s.Subscriptions(); len(subs) != 2 || len(subs[0].Nodes) != 2 || len(first.Nodes) != 1 {
		t.Errorf("subscriptions = %+v, first = %+v", subs, first)
	}
}

func TestPastedList(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	sub, err := s.Add(context.Background(), AddRequest{Content: pasted, Name: "Mine"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.URL != "" || len(sub.Nodes) != 2 || sub.DisplayName() != "Mine" {
		t.Fatalf("sub = %+v", sub)
	}
	if _, err := s.Refresh(context.Background(), sub.ID); err == nil {
		t.Error("refreshed a pasted list")
	}
	content := nodeB
	sub, err = s.Edit(context.Background(), sub.ID, Edit{Content: &content})
	if err != nil || len(sub.Nodes) != 1 {
		t.Fatalf("edit content: %v %+v", err, sub)
	}
	if _, err := s.Add(context.Background(), AddRequest{Content: "hello"}); err == nil {
		t.Error("added a list without nodes")
	}
	if len(f.panel.calls) != 0 {
		t.Error("a pasted list was fetched")
	}
}

func TestSelectionFollowsRefresh(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted)
	s := f.open(t)
	sub, err := s.Add(context.Background(), AddRequest{URL: subURL})
	if err != nil {
		t.Fatal(err)
	}
	fpA := sub.Nodes[0].Fingerprint()
	if _, err := s.Select(sub.ID, "nope", ""); err == nil {
		t.Error("selected a missing node")
	}
	if _, err := s.Select(sub.ID, fpA, ""); err != nil {
		t.Fatal(err)
	}

	// The panel rotated Amsterdam's credentials: the name still matches.
	f.panel.set(subURL, nodeA2+"\n"+nodeB)
	if _, err := s.Refresh(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	sel, n, ok := s.Selected()
	if !ok || n.Name != "Amsterdam" || n.Password != "pw2" || sel.Fingerprint == fpA || sel.Fingerprint != n.Fingerprint() {
		t.Fatalf("after refresh: %+v %+v %v", sel, n, ok)
	}

	// Amsterdam is gone: the selection stays, unresolved.
	f.panel.set(subURL, nodeB)
	s.Refresh(context.Background(), sub.ID)
	if sel, _, ok := s.Selected(); ok || sel.Name != "Amsterdam" {
		t.Errorf("after removal: %+v %v", sel, ok)
	}

	if err := s.Remove(sub.ID); err != nil {
		t.Fatal(err)
	}
	if sel, _, _ := s.Selected(); sel.Subscription != "" {
		t.Errorf("selection survived its subscription: %+v", sel)
	}
	if !slices.Equal(f.whats(), []string{"subscription-added", "selection", "subscription-updated", "subscription-updated", "subscription-removed"}) {
		t.Errorf("changes = %v", f.whats())
	}
}

func TestFailedRefreshKeepsNodes(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted)
	s := f.open(t)
	sub, _ := s.Add(context.Background(), AddRequest{URL: subURL})

	f.panel.fail = errors.New("fetch subscription: server returned 503 Service Unavailable")
	f.clock.add(time.Hour)
	got, err := s.Refresh(context.Background(), sub.ID)
	if err == nil || len(got.Nodes) != 2 || !strings.Contains(got.LastError, "503") || !got.UpdatedAt.Equal(sub.UpdatedAt) {
		t.Fatalf("failed refresh: %v %+v", err, got)
	}
	f.mu.Lock()
	last := f.changes[len(f.changes)-1]
	f.mu.Unlock()
	if last.Err == nil {
		t.Error("failure not reported")
	}

	f.panel.fail = nil
	f.panel.set(subURL, "")
	if _, err := s.Refresh(context.Background(), sub.ID); err == nil {
		t.Error("an empty response replaced the nodes")
	}
	if got, _ := s.Subscription(sub.ID); len(got.Nodes) != 2 {
		t.Errorf("nodes = %d", len(got.Nodes))
	}
}

func TestEditRefetchesNewURL(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted)
	newURL := "https://panel.example/sub/NEW"
	f.panel.set(newURL, nodeB)
	s := f.open(t)
	sub, _ := s.Add(context.Background(), AddRequest{URL: subURL})

	name := " Work "
	sub, err := s.Edit(context.Background(), sub.ID, Edit{Name: &name})
	if err != nil || sub.Name != "Work" || len(f.panel.calls) != 1 {
		t.Fatalf("rename: %v %+v, %d fetches", err, sub, len(f.panel.calls))
	}
	bad := "https://panel.example/sub/MISSING"
	if _, err := s.Edit(context.Background(), sub.ID, Edit{URL: &bad}); err == nil {
		t.Error("saved a URL that does not work")
	}
	if got, _ := s.Subscription(sub.ID); got.URL != subURL {
		t.Errorf("URL = %s", got.URL)
	}
	sub, err = s.Edit(context.Background(), sub.ID, Edit{URL: &newURL})
	if err != nil || sub.URL != newURL || len(sub.Nodes) != 1 || sub.Name != "Work" {
		t.Fatalf("new URL: %v %+v", err, sub)
	}
}

func TestMove(t *testing.T) {
	s := newFixture(t).open(t)
	var ids []string
	for _, n := range []string{"a", "b", "c"} {
		sub, err := s.Add(context.Background(), AddRequest{Content: nodeA, Name: n})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sub.ID)
	}
	if err := s.Move(ids[2], 0); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, sub := range s.Subscriptions() {
		names = append(names, sub.Name)
	}
	if !slices.Equal(names, []string{"c", "a", "b"}) {
		t.Errorf("order = %v", names)
	}
}

func TestRefreshDue(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted)
	s := f.open(t)
	sub, _ := s.Add(context.Background(), AddRequest{URL: subURL}) // no panel interval: 12 h
	s.Add(context.Background(), AddRequest{Content: pasted})
	ctx := context.Background()

	f.clock.add(11 * time.Hour)
	if n := s.RefreshDue(ctx); n != 0 {
		t.Errorf("refreshed %d early", n)
	}
	f.clock.add(time.Hour)
	if n := s.RefreshDue(ctx); n != 1 {
		t.Errorf("refreshed %d at 12 h, want 1", n)
	}

	f.panel.fail = errors.New("offline")
	f.clock.add(12 * time.Hour)
	s.RefreshDue(ctx)
	f.clock.add(10 * time.Minute)
	if n := s.RefreshDue(ctx); n != 0 {
		t.Error("retried a failure too soon")
	}
	f.clock.add(5 * time.Minute)
	if n := s.RefreshDue(ctx); n != 1 {
		t.Error("did not retry a failure")
	}

	set := s.Settings()
	set.Updates.Auto = false
	s.SetSettings(set)
	f.clock.add(48 * time.Hour)
	if n := s.RefreshDue(ctx); n != 0 {
		t.Error("refreshed with auto-update off")
	}
	_ = sub
}

func TestUnusableFileIsMovedAside(t *testing.T) {
	f := newFixture(t)
	os.MkdirAll(filepath.Dir(f.path), 0o700)
	os.WriteFile(f.path, []byte("{not json"), 0o600)
	s, err := Open(f.path, f.options())
	if !errors.Is(err, ErrReset) || s == nil {
		t.Fatalf("Open = %v, %v", s, err)
	}
	if !s.Settings().TUN {
		t.Error("no defaults after reset")
	}
	matches, _ := filepath.Glob(f.path + ".bad-*")
	if len(matches) != 1 {
		t.Errorf("moved-aside files: %v", matches)
	}
}

func TestInvalidSettingsInFileKeepSubscriptions(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	s.Add(context.Background(), AddRequest{Content: pasted})
	b, _ := os.ReadFile(f.path)
	os.WriteFile(f.path, []byte(strings.Replace(string(b), `"mode": "auto"`, `"mode": "turbo"`, 1)), 0o600)

	s, err := Open(f.path, f.options())
	if !errors.Is(err, ErrReset) || !strings.Contains(err.Error(), "turbo") {
		t.Fatalf("err = %v", err)
	}
	if len(s.Subscriptions()) != 1 || s.Settings().Cores.Mode != ModeAuto {
		t.Errorf("subscriptions %d, mode %s", len(s.Subscriptions()), s.Settings().Cores.Mode)
	}
	// The file stays until the next save; a copy keeps the lost settings.
	if matches, _ := filepath.Glob(f.path + ".bad-*"); len(matches) != 1 {
		t.Errorf("copies: %v", matches)
	}
}

func TestOldRussiaPresetIsMigrated(t *testing.T) {
	s := newFixture(t).open(t)
	set := s.Settings()
	set.Routing.DirectDomains = []string{"ru", "bank.example", "su", "xn--p1ai"}
	saved, err := s.SetSettings(set)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Routing.RussiaDirect || !slices.Equal(saved.Routing.DirectDomains, []string{"bank.example"}) {
		t.Errorf("routing = %+v", saved.Routing)
	}
}

func TestSelectTwinNodes(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	// Two names for the same server, as panels do for an "auto" entry.
	twin := "trojan://pw@203.0.113.5:443?sni=a.example.com#Auto"
	sub, err := s.Add(context.Background(), AddRequest{Content: twin + "\n" + nodeA})
	if err != nil {
		t.Fatal(err)
	}
	fp := sub.Nodes[0].Fingerprint()
	if sub.Nodes[1].Fingerprint() != fp {
		t.Fatal("twins differ in fingerprint")
	}
	if sel, err := s.Select(sub.ID, fp, "Amsterdam"); err != nil || sel.Name != "Amsterdam" {
		t.Fatalf("select: %+v %v", sel, err)
	}
	if _, n, ok := s.Selected(); !ok || n.Name != "Amsterdam" {
		t.Errorf("selected %q", n.Name)
	}
	// Without a name, or with a stale one, the first twin is taken.
	if sel, err := s.Select(sub.ID, fp, "Gone"); err != nil || sel.Name != "Auto" {
		t.Errorf("stale name: %+v %v", sel, err)
	}
}

func TestRoutingLists(t *testing.T) {
	s := newFixture(t).open(t)
	set := s.Settings()
	if set.Routing.Mode != RouteAll {
		t.Fatalf("default mode = %q", set.Routing.Mode)
	}
	set.Routing.Mode = " Selected "
	set.Routing.ProxyDomains = []string{"YouTube.com", "youtube.com.", " googlevideo.com "}
	set.Routing.BlockDomains = []string{"ads.example"}
	set.Routing.ProxyApps = []string{`C:\Apps\Telegram.exe`, "telegram.exe"}
	set.Routing.ProxyIPs = []string{" 91.108.4.0/22 ", "91.108.5.9/22", "2001:67c:4e8::/48"}
	set.Routing.DirectIPs = []string{"203.0.113.7", "203.0.113.7/32", "10.8.1.2/16"}
	saved, err := s.SetSettings(set)
	if err != nil {
		t.Fatal(err)
	}
	r := saved.Routing
	for _, c := range []struct {
		name      string
		got, want []string
	}{
		{"proxy_domains", r.ProxyDomains, []string{"youtube.com", "googlevideo.com"}},
		{"block_domains", r.BlockDomains, []string{"ads.example"}},
		{"proxy_apps", r.ProxyApps, []string{"Telegram.exe"}},
		{"proxy_ips", r.ProxyIPs, []string{"91.108.4.0/22", "2001:67c:4e8::/48"}},
		{"direct_ips", r.DirectIPs, []string{"203.0.113.7", "10.8.0.0/16"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if r.Mode != RouteSelected {
		t.Errorf("mode = %q", r.Mode)
	}

	set = saved
	set.Routing.Mode = "some"
	set.Routing.DirectIPs = []string{"300.1.1.1", "198.18.0.0/16", "0.0.0.0/0"}
	set.Routing.ProxyDomains = []string{"bad domain"}
	_, err = s.SetSettings(set)
	for _, want := range []string{"routing.mode", "300.1.1.1", "198.18.0.0/16", "0.0.0.0/0", "routing.proxy_domains"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

// editFile changes the store file the way a later version would write it.
func editFile(t *testing.T, path string, f func(m map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	if b, err = json.Marshal(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLaterVersionsFieldsSurvive(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	if _, err := s.Add(context.Background(), AddRequest{Content: pasted}); err != nil {
		t.Fatal(err)
	}
	editFile(t, f.path, func(m map[string]any) {
		m["future"] = map[string]any{"n": 12345678901234567}
		set := m["settings"].(map[string]any)
		set["future_setting"] = true
		set["routing"].(map[string]any)["smart_lists"] = []any{"a", "b"}
		m["subscriptions"].([]any)[0].(map[string]any)["pinned"] = true
	})

	// An earlier version changes a setting of its own.
	s = f.open(t)
	set := s.Settings()
	set.TUN = false
	if _, err := s.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f.path)
	for _, want := range []string{`"n": 12345678901234567`, `"future_setting": true`, `"smart_lists"`, `"pinned": true`, `"tun": false`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lost:\n%s", want, b)
		}
	}
	if s := f.open(t); s.Settings().TUN || len(s.Subscriptions()) != 1 {
		t.Errorf("reopened: tun %v, %d subscriptions", s.Settings().TUN, len(s.Subscriptions()))
	}
}

func TestClearedFieldsStayCleared(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	sub, err := s.Add(context.Background(), AddRequest{Content: pasted})
	if err != nil {
		t.Fatal(err)
	}
	n := sub.Nodes[0]
	if _, err := s.Select(sub.ID, n.Fingerprint(), n.Name); err != nil {
		t.Fatal(err)
	}
	editFile(t, f.path, func(m map[string]any) { m["future"] = true })

	s = f.open(t)
	if err := s.Remove(sub.ID); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f.path)
	if strings.Contains(string(b), `"selection"`) || !strings.Contains(string(b), `"future": true`) {
		t.Errorf("the removed selection came back or the unknown field was lost:\n%s", b)
	}
}
