package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/ruleset"
)

// Where GitHub and jsDelivr are both out of reach, CoreShift's own sets and
// their manifest come from the branch on the GitLab mirror.
func TestPublishedSetsFromGitLab(t *testing.T) {
	blocked := geoSet{Tag: "geosite-ru-blocked", Proxy: true}
	if got := ruleset.Sources(blocked.Tag); len(got) != 3 || got[2] != "https://gitlab.com/NezZeen/coreshift/-/raw/rulesets/geosite-ru-blocked.srs" {
		t.Fatalf("sources %v", got)
	}
	if got := ruleset.Sources("geoip-ru"); len(got) != 1 || strings.Contains(got[0], "gitlab") {
		t.Errorf("SagerNet's sets from the mirror: %v", got)
	}
	names := []string{"meduza.io", "linkedin.com"}
	for i := range 100 {
		names = append(names, fmt.Sprintf("blocked-%d.example", i))
	}
	builtin := namesSet(t, names...)
	update := namesSet(t, append(names, "new-blocked.example")...)
	gitlab := ruleset.PublishedGitLab + blocked.Tag + ".srs"
	manifest := ruleset.PublishedGitLab + ruleset.PublishedManifest

	rig := newRuleSetsRig(t, time.Now().Add(-48*time.Hour))
	rig.baseline = func(tag string) ([]byte, time.Time, bool) {
		if tag == blocked.Tag {
			return builtin, rig.fetched, true
		}
		return nil, time.Time{}, false
	}
	var mu sync.Mutex
	served := map[string][]byte{}
	var asked []string
	rig.fetch = func(_ context.Context, rawURL string, _ *url.URL) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, rawURL)
		if b, ok := served[rawURL]; ok {
			return b, nil
		}
		return nil, errors.New("dial tcp: i/o timeout")
	}
	path := rig.path(blocked)
	if _, ok := rig.ready(blocked, path); !ok {
		t.Fatal("no built-in copy")
	}
	rig.events = nil

	// Only GitLab answers: its copy is taken.
	mu.Lock()
	served = map[string][]byte{gitlab: update}
	mu.Unlock()
	rig.refresh(blocked, path, nil)
	if !bytes.Equal(rig.onDisk(t, blocked), update) {
		t.Error("the GitLab copy was not taken")
	}
	if e := rig.last(t); e.Line != "updated" {
		t.Errorf("event %+v", e)
	}

	// Its manifest says the copy is current: nothing else is downloaded.
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, old, old)
	m, _ := json.Marshal(ruleset.Manifest{Fetched: time.Now(), Sets: map[string]ruleset.File{blocked.Tag: ruleset.Describe(update)}})
	mu.Lock()
	served, asked = map[string][]byte{manifest: m}, nil
	rig.events = nil
	mu.Unlock()
	rig.refresh(blocked, path, nil)
	want := []string{ruleset.Published + ruleset.PublishedManifest, ruleset.PublishedMirror + ruleset.PublishedManifest, manifest}
	if !slices.Equal(asked, want) || !fresh(path) || len(rig.events) != 0 {
		t.Errorf("asked %v, events %+v", asked, rig.events)
	}
}
