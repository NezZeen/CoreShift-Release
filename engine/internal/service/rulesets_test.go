package service

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"coreshift/engine/internal/ruleset"
)

// editSet returns the built-in tag with its first rule changed by f.
func editSet(t *testing.T, tag string, f func(r *option.DefaultHeadlessRule)) []byte {
	t.Helper()
	b, _, ok := ruleset.Baseline(tag)
	if !ok {
		t.Fatalf("no built-in %s", tag)
	}
	rs, err := srs.Read(bytes.NewReader(b), true)
	if err != nil {
		t.Fatal(err)
	}
	r := rs.Options.Rules[0].DefaultOptions
	r.DomainMatcher, r.IPSet = nil, nil // written from the lists
	f(&r)
	rs.Options.Rules[0].DefaultOptions = r
	var buf bytes.Buffer
	if err := srs.Write(&buf, rs.Options, C.RuleSetVersion1); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type ruleSetsRig struct {
	*ruleSets
	upstream []byte // what the source serves
	fetched  time.Time
	events   []Event
}

// newRuleSetsRig keeps sets in a temporary folder, with built-in copies
// downloaded at fetched.
func newRuleSetsRig(t *testing.T, fetched time.Time) *ruleSetsRig {
	rig := &ruleSetsRig{fetched: fetched}
	rig.ruleSets = &ruleSets{
		dir: t.TempDir(),
		fetch: func(context.Context, string, *url.URL) ([]byte, error) {
			return rig.upstream, nil
		},
		baseline: func(tag string) ([]byte, time.Time, bool) {
			b, _, ok := ruleset.Baseline(tag)
			return b, rig.fetched, ok
		},
		publish:    func(e Event) { rig.events = append(rig.events, e) },
		refreshing: map[string]bool{},
	}
	return rig
}

func (rig *ruleSetsRig) path(gs geoSet) string { return filepath.Join(rig.dir, gs.Tag+".srs") }

func (rig *ruleSetsRig) onDisk(t *testing.T, gs geoSet) []byte {
	t.Helper()
	b, err := os.ReadFile(rig.path(gs))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// last returns the last event and forgets them all.
func (rig *ruleSetsRig) last(t *testing.T) Event {
	t.Helper()
	if len(rig.events) == 0 {
		t.Fatal("no event")
	}
	e := rig.events[len(rig.events)-1]
	rig.events = nil
	return e
}

func TestRuleSetsStartFromBuiltIn(t *testing.T) {
	fetched := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	rig := newRuleSetsRig(t, fetched)
	for _, gs := range append(slices.Clone(russiaSets), adsSet) {
		if !slices.Contains(ruleset.Tags(), gs.Tag) {
			continue // a set this build does not carry yet
		}
		mod, ok := rig.ready(gs, rig.path(gs))
		want, _, _ := ruleset.Baseline(gs.Tag)
		if !ok || !mod.Equal(fetched) || !bytes.Equal(rig.onDisk(t, gs), want) {
			t.Errorf("%s: ready %v at %v", gs.Tag, ok, mod)
		}
		if fi, err := os.Stat(rig.path(gs)); err != nil || !fi.ModTime().Equal(fetched) {
			t.Errorf("%s: not dated as downloaded: %v", gs.Tag, err)
		}
		if e := rig.last(t); e.Line != "builtin" || e.Error != "" {
			t.Errorf("%s: event %+v", gs.Tag, e)
		}
	}
	// Old: connecting refreshes them in the background, through the proxy.
	var via []string
	done := make(chan string, len(russiaSets))
	rig.fetch = func(_ context.Context, rawURL string, proxy *url.URL) ([]byte, error) {
		if proxy != nil {
			via = append(via, proxy.Host)
		}
		return upstreamSet(rawURL)
	}
	rig.publish = func(e Event) { done <- e.Line }
	rig.get(context.Background(), russiaSets[:1], &url.URL{Scheme: "socks5", Host: "127.0.0.1:1"})
	select {
	case line := <-done:
		if line != "updated" || len(via) != 1 {
			t.Errorf("refresh: %s via %v", line, via)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an old set was not refreshed")
	}
}

func TestRuleSetUpdatesChecked(t *testing.T) {
	rig := newRuleSetsRig(t, time.Now().Add(-30*24*time.Hour))
	ru, ip, google := russiaSets[0], russiaSets[1], googleSet
	for _, gs := range []geoSet{ru, ip, google} {
		rig.ready(gs, rig.path(gs))
	}
	rig.events = nil
	builtinIP := rig.onDisk(t, ip)

	// What a tampered or broken source could serve: each leaves the
	// previous copy in place, and says so.
	tampered := []struct {
		name string
		gs   geoSet
		b    []byte
	}{
		{"error page", ip, []byte("<html>rate limited</html>")},
		{"truncated", ip, builtinIP[:len(builtinIP)/3]},
		{"shrunk", ip, editSet(t, ip.Tag, func(r *option.DefaultHeadlessRule) {
			r.IPCIDR = []string{"77.88.8.0/24", "87.250.250.0/24"}
		})},
		{"with Google's DNS", ip, editSet(t, ip.Tag, func(r *option.DefaultHeadlessRule) {
			r.IPCIDR = append(r.IPCIDR, "8.8.8.0/24")
		})},
		{"Google without google.com", google, editSet(t, google.Tag, func(r *option.DefaultHeadlessRule) {
			r.DomainSuffix = slices.DeleteFunc(r.DomainSuffix, func(s string) bool { return s == "google.com" })
		})},
		{"Russian with every .com", ru, editSet(t, ru.Tag, func(r *option.DefaultHeadlessRule) {
			r.DomainSuffix = append(r.DomainSuffix, "com")
		})},
	}
	for _, c := range tampered {
		before := rig.onDisk(t, c.gs)
		rig.upstream = c.b
		rig.refresh(c.gs, rig.path(c.gs), nil)
		if !bytes.Equal(rig.onDisk(t, c.gs), before) {
			t.Errorf("%s: replaced the previous copy", c.name)
		}
		if e := rig.last(t); e.Line != "kept" || !strings.Contains(e.Error, "не обновилась") {
			t.Errorf("%s: event %+v", c.name, e)
		}
	}

	// A usual update is taken...
	upd := editSet(t, ru.Tag, func(r *option.DefaultHeadlessRule) {
		r.DomainSuffix = append(slices.Delete(r.DomainSuffix, 100, 105), "new-russian-shop.com")
	})
	rig.upstream = upd
	rig.refresh(ru, rig.path(ru), nil)
	if !bytes.Equal(rig.onDisk(t, ru), upd) {
		t.Error("a valid update was not taken")
	}
	if e := rig.last(t); e.Line != "updated" || e.Error != "" {
		t.Errorf("update: event %+v", e)
	}
	mod, ok := rig.ready(ru, rig.path(ru))
	if !ok || time.Since(mod) > time.Minute || !bytes.Equal(rig.onDisk(t, ru), upd) {
		t.Errorf("the update gave way to the older built-in copy: %v %v", ok, mod)
	}
}

func TestRuleSetOnDiskChecked(t *testing.T) {
	fetched := time.Now().Add(-24 * time.Hour)
	rig := newRuleSetsRig(t, fetched)
	gs := russiaSets[0]
	path := rig.path(gs)
	builtin, _, _ := ruleset.Baseline(gs.Tag)
	write := func(b []byte, mod time.Time) {
		t.Helper()
		if err := os.MkdirAll(rig.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(path, mod, mod)
	}

	// Damaged: the built-in copy takes its place.
	write([]byte("SRS\x01garbage"), time.Now())
	if _, ok := rig.ready(gs, path); !ok || !bytes.Equal(rig.onDisk(t, gs), builtin) {
		t.Error("a damaged set was kept")
	}
	if e := rig.events[0]; e.Line != "damaged" || e.Error == "" {
		t.Errorf("event %+v", e)
	}

	// Valid, but older than the built-in copy (CoreShift was updated):
	// the built-in copy.
	older := editSet(t, gs.Tag, func(r *option.DefaultHeadlessRule) { r.Domain = append(r.Domain, "old.example.ru") })
	write(older, fetched.Add(-time.Hour))
	if mod, ok := rig.ready(gs, path); !ok || !mod.Equal(fetched) || !bytes.Equal(rig.onDisk(t, gs), builtin) {
		t.Error("an older set was kept over the built-in copy")
	}

	// Valid and newer: kept.
	write(older, fetched.Add(time.Hour))
	if _, ok := rig.ready(gs, path); !ok || !bytes.Equal(rig.onDisk(t, gs), older) {
		t.Error("a newer set gave way to the built-in copy")
	}
}
