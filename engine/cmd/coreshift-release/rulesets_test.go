package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"google.golang.org/protobuf/encoding/protowire"

	"coreshift/engine/internal/ruleset"
)

// builtInDir copies the built-in sets into a temporary folder.
func builtInDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	m := ruleset.Manifest{Fetched: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Sets: map[string]ruleset.File{}}
	for _, tag := range ruleset.Tags() {
		b, _, _ := ruleset.Baseline(tag)
		if err := os.WriteFile(filepath.Join(dir, tag+".srs"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		m.Sets[tag] = ruleset.Describe(b)
	}
	body, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, ruleset.ManifestName), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func tagOf(url string) string { return strings.TrimSuffix(path.Base(url), ".srs") }

// shrunkIP is geoip-ru down to Yandex: a valid set, far from the built-in one.
func shrunkIP(t *testing.T) []byte {
	b, _, _ := ruleset.Baseline("geoip-ru")
	rs, err := srs.Read(bytes.NewReader(b), true)
	if err != nil {
		t.Fatal(err)
	}
	r := rs.Options.Rules[0].DefaultOptions
	r.IPSet, r.IPCIDR = nil, []string{"77.88.8.0/24", "87.250.250.0/24"}
	rs.Options.Rules[0].DefaultOptions = r
	var buf bytes.Buffer
	if err := srs.Write(&buf, rs.Options, C.RuleSetVersion1); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// geoList is a v2ray list (geosite.dat) of categories of domains, and
// its .sha256sum.
func geoList(cats map[string][]string) (dat, sum []byte) {
	for code, names := range cats {
		var entry []byte
		entry = protowire.AppendTag(entry, 1, protowire.BytesType)
		entry = protowire.AppendString(entry, strings.ToUpper(code))
		for _, n := range names {
			var d []byte
			d = protowire.AppendTag(d, 1, protowire.VarintType)
			d = protowire.AppendVarint(d, 2) // a domain and below
			d = protowire.AppendTag(d, 2, protowire.BytesType)
			d = protowire.AppendString(d, n)
			entry = protowire.AppendTag(entry, 2, protowire.BytesType)
			entry = protowire.AppendBytes(entry, d)
		}
		dat = protowire.AppendTag(dat, 1, protowire.BytesType)
		dat = protowire.AppendBytes(dat, entry)
	}
	h := sha256.Sum256(dat)
	return dat, []byte(hex.EncodeToString(h[:]) + "  geosite.dat\n")
}

var goodList = map[string][]string{
	"category-ads-all": {"doubleclick.net", "ads.example.org"},
	"ru-blocked":       {"meduza.io", "linkedin.com", "rutracker.org"},
	"ru-blocked-all":   {"meduza.io", "linkedin.com", "rutracker.org", "more.example"},
}

// serveList answers for runetfreedom's list, and its checksum.
func serveList(dat, sum []byte, next func(string) ([]byte, error)) func(string) ([]byte, error) {
	return func(url string) ([]byte, error) {
		switch url {
		case ruleset.RunetFreedomDat:
			return dat, nil
		case ruleset.RunetFreedomDat + ".sha256sum":
			return sum, nil
		}
		return next(url)
	}
}

// Sets CoreShift is given are downloaded for the first time, checked on
// their own; the ones made out of runetfreedom's list, from the list whose
// checksum matches. A bad one stops the run.
func TestRefreshRuleSetsNew(t *testing.T) {
	dir := builtInDir(t)
	m := ruleset.Manifest{Fetched: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Sets: map[string]ruleset.File{}}
	b, _, _ := ruleset.Baseline("geoip-ru")
	m.Sets["geoip-ru"] = ruleset.Describe(b)
	body, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(dir, ruleset.ManifestName), body, 0o644)
	baseline := func(url string) ([]byte, error) {
		b, _, _ := ruleset.Baseline(tagOf(url))
		return b, nil
	}

	bad, badSum := geoList(map[string][]string{"category-ads-all": {"doubleclick.net", "google.com"}, "ru-blocked": {"meduza.io", "linkedin.com"}})
	if err := refreshRuleSets(dir, serveList(bad, badSum, baseline), true, io.Discard); err == nil || !strings.Contains(err.Error(), "geosite-category-ads-all (new): has google.com") {
		t.Fatalf("an ad list with google.com: %v", err)
	}
	good, sum := geoList(goodList)
	if err := refreshRuleSets(dir, serveList(good, badSum, baseline), true, io.Discard); err == nil || !strings.Contains(err.Error(), "sha256sum") {
		t.Fatalf("a list that does not match its checksum: %v", err)
	}
	var out bytes.Buffer
	if err := refreshRuleSets(dir, serveList(good, sum, baseline), false, &out); err != nil {
		t.Fatal(err)
	}
	want, err := ruleset.FromDat(good, false, "ru-blocked")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "geosite-ru-blocked.srs")); !bytes.Equal(got, want) || !strings.Contains(out.String(), "geosite-category-ads-all: new") {
		t.Errorf("new sets not written:\n%s", out.String())
	}
}

func TestRefreshRuleSets(t *testing.T) {
	dir := builtInDir(t)
	manifest := func() ruleset.Manifest {
		var m ruleset.Manifest
		b, _ := os.ReadFile(filepath.Join(dir, ruleset.ManifestName))
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	before := manifest()
	shrunk := shrunkIP(t)
	// Sets CoreShift does not carry a copy of yet come out of a made-up
	// list, with what their checks want.
	dat, sum := geoList(goodList)
	serve := func(over map[string][]byte) func(string) ([]byte, error) {
		return serveList(dat, sum, func(url string) ([]byte, error) {
			if b, ok := over[tagOf(url)]; ok {
				return b, nil
			}
			b, _, _ := ruleset.Baseline(tagOf(url))
			return b, nil
		})
	}

	// One set far from its previous copy: nothing is written.
	err := refreshRuleSets(dir, serve(map[string][]byte{"geoip-ru": shrunk}), false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "geoip-ru") {
		t.Fatalf("err = %v", err)
	}
	if m := manifest(); !m.Fetched.Equal(before.Fetched) {
		t.Error("sets.json written despite a rejected set")
	}
	// Damaged: not even with -accept.
	if err := refreshRuleSets(dir, serve(map[string][]byte{"geoip-ru": []byte("<html>")}), true, io.Discard); err == nil {
		t.Fatal("a damaged set was taken")
	}
	// -accept takes the far one.
	if err := refreshRuleSets(dir, serve(map[string][]byte{"geoip-ru": shrunk}), true, io.Discard); err != nil {
		t.Fatal(err)
	}
	m := manifest()
	if got, _ := os.ReadFile(filepath.Join(dir, "geoip-ru.srs")); !bytes.Equal(got, shrunk) || m.Sets["geoip-ru"] != ruleset.Describe(shrunk) {
		t.Error("the accepted set was not written")
	}
	if !m.Fetched.After(before.Fetched) {
		t.Error("sets.json not dated")
	}
	// The usual run: the same sets again.
	var out bytes.Buffer
	if err := refreshRuleSets(dir, serve(map[string][]byte{"geoip-ru": shrunk}), false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "geosite-google: unchanged") {
		t.Errorf("output:\n%s", out.String())
	}
}

// rulesets-publish writes the sets made out of runetfreedom's list and
// their manifest, and nothing when they have not changed.
func TestPublishRuleSets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rulesets")
	noBaseline := func(string) ([]byte, time.Time, bool) { return nil, time.Time{}, false }
	unreachable := func(url string) ([]byte, error) { t.Fatalf("asked for %s", url); return nil, nil }
	read := func(name string) []byte {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return b
	}

	dat, sum := geoList(goodList)
	if err := publishRuleSets(dir, serveList(dat, sum, unreachable), noBaseline, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	var m ruleset.Manifest
	if err := json.Unmarshal(read(ruleset.PublishedManifest), &m); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ tag, category string }{{"geosite-ru-blocked", "ru-blocked"}, {"geosite-category-ads-all", "category-ads-all"}} {
		want, err := ruleset.FromDat(dat, false, c.category)
		if err != nil {
			t.Fatal(err)
		}
		if b := read(c.tag + ".srs"); !bytes.Equal(b, want) || m.Sets[c.tag] != ruleset.Describe(want) {
			t.Errorf("%s not published", c.tag)
		}
	}
	if len(m.Sets) != 2 || m.Fetched.IsZero() {
		t.Errorf("manifest %+v", m)
	}

	// The same list again: not a byte changes, so the branch gets no commit.
	before := read(ruleset.PublishedManifest)
	if err := publishRuleSets(dir, serveList(dat, sum, unreachable), noBaseline, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(read(ruleset.PublishedManifest), before) {
		t.Error("an unchanged list rewrote the manifest")
	}

	// A list that does not match its checksum, and one devices would refuse.
	bad := map[string][]string{"category-ads-all": {"doubleclick.net"}, "ru-blocked": {"meduza.io", "linkedin.com", "vk.com"}}
	badDat, badSum := geoList(bad)
	if err := publishRuleSets(dir, serveList(badDat, sum, unreachable), noBaseline, false, io.Discard); err == nil || !strings.Contains(err.Error(), "sha256sum") {
		t.Errorf("checksum: %v", err)
	}
	if err := publishRuleSets(dir, serveList(badDat, badSum, unreachable), noBaseline, true, io.Discard); err == nil || !strings.Contains(err.Error(), "has vk.com") {
		t.Errorf("vk.com in ru-blocked: %v", err)
	}
	if !bytes.Equal(read(ruleset.PublishedManifest), before) {
		t.Error("a refused list was published")
	}

	// A change: published.
	more := map[string][]string{"category-ads-all": goodList["category-ads-all"], "ru-blocked": append(goodList["ru-blocked"], "new.example")}
	newDat, newSum := geoList(more)
	var out bytes.Buffer
	if err := publishRuleSets(dir, serveList(newDat, newSum, unreachable), noBaseline, false, &out); err != nil {
		t.Fatal(err)
	}
	want, _ := ruleset.FromDat(newDat, false, "ru-blocked")
	if !bytes.Equal(read("geosite-ru-blocked.srs"), want) || bytes.Equal(read(ruleset.PublishedManifest), before) ||
		!strings.Contains(out.String(), "geosite-category-ads-all: unchanged") {
		t.Errorf("the change was not published:\n%s", out.String())
	}
}
