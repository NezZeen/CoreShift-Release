package main

import (
	"bytes"
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
	serve := func(over map[string][]byte) func(string) ([]byte, error) {
		return func(url string) ([]byte, error) {
			if b, ok := over[tagOf(url)]; ok {
				return b, nil
			}
			b, _, _ := ruleset.Baseline(tagOf(url))
			return b, nil
		}
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
