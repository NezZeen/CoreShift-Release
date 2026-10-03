package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fill puts one element into every slice and map reachable from v.
func fill(t *testing.T, v reflect.Value, path string) int {
	n := 0
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				n += fill(t, f, path+"."+v.Type().Field(i).Name)
			}
		}
	case reflect.Slice:
		e := reflect.New(v.Type().Elem()).Elem()
		fill(t, e, path+"[]")
		v.Set(reflect.Append(reflect.MakeSlice(v.Type(), 0, 4), e))
		n++
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		m.SetMapIndex(k, e)
		v.Set(m)
		n++
	case reflect.String:
		v.SetString("orig")
	}
	return n
}

// Every slice and map of Settings is copied: one the copy shares would be
// changed through it.
func TestCloneSettingsCopiesEverySliceAndMap(t *testing.T) {
	var orig Settings
	if n := fill(t, reflect.ValueOf(&orig).Elem(), "Settings"); n < 9 {
		t.Fatalf("only %d slices and maps found", n)
	}
	before, _ := json.Marshal(orig)
	c := cloneSettings(orig)
	var mutate func(v reflect.Value)
	mutate = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Struct:
			for i := range v.NumField() {
				if f := v.Field(i); f.CanSet() {
					mutate(f)
				}
			}
		case reflect.Slice:
			for i := range v.Len() {
				if e := v.Index(i); e.Kind() == reflect.String {
					e.SetString("changed")
				} else {
					mutate(e)
				}
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				v.SetMapIndex(k, reflect.Zero(v.Type().Elem()))
			}
		}
	}
	mutate(reflect.ValueOf(&c).Elem())
	if after, _ := json.Marshal(orig); string(after) != string(before) {
		t.Errorf("changing the copy changed the original:\nbefore %s\nafter  %s", before, after)
	}
	// Nil and empty stay apart.
	var empty Settings
	empty.Routing.FilterApps = []string{}
	if got := cloneSettings(empty); got.Routing.FilterApps == nil || got.Routing.DirectApps != nil {
		t.Errorf("nil/empty not kept: %#v %#v", got.Routing.FilterApps, got.Routing.DirectApps)
	}
}

// What PUT /v1/settings does: decode the request into the current settings.
// A request that is then rejected must leave the stored settings alone;
// encoding/json writes into the slices it finds.
func TestDecodingIntoSettingsLeavesTheStoreAlone(t *testing.T) {
	s := newFixture(t).open(t)
	set := s.Settings()
	set.Routing.AppFilter, set.Routing.FilterApps = AppsExclude, []string{"org.example.one", "org.example.two"}
	if _, err := s.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	got := s.Settings()
	if err := json.Unmarshal([]byte(`{"routing":{"filter_apps":["evil.one","evil.two"],"direct_apps":["x.exe"]},"cores":{"priority":["xray"]}}`), &got); err != nil {
		t.Fatal(err)
	}
	// Not saved: as when SetSettings rejects it.
	now := s.Settings()
	if strings.Join(now.Routing.FilterApps, ",") != "org.example.one,org.example.two" || len(now.Routing.DirectApps) != 0 ||
		len(now.Cores.Priority) != 3 {
		t.Errorf("the store changed: %+v %+v", now.Routing, now.Cores.Priority)
	}
}

func TestHWIDScope(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted)
	s := f.open(t)
	sub, err := s.Add(context.Background(), AddRequest{URL: subURL})
	if err != nil {
		t.Fatal(err)
	}
	if sub.HWIDScope != HWIDPanel || f.panel.legacy[0] {
		t.Errorf("a new subscription: scope %q, legacy %v", sub.HWIDScope, f.panel.legacy)
	}

	// A subscription saved by an earlier version keeps the machine-wide id.
	b, _ := os.ReadFile(f.path)
	b = []byte(strings.Replace(string(b), `"hwid_scope": "panel",`, "", 1))
	if strings.Contains(string(b), "hwid_scope") {
		t.Fatal("hwid_scope not removed")
	}
	os.WriteFile(f.path, b, 0o600)
	s = f.open(t)
	if _, err := s.Refresh(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	if !f.panel.legacy[1] {
		t.Error("an old subscription sent the per-panel id")
	}
	// Its new link on the same panel: still the old id.
	sameHost := "https://panel.example/sub/ROTATED"
	f.panel.set(sameHost, pasted)
	sub, err = s.Edit(context.Background(), sub.ID, Edit{URL: &sameHost})
	if err != nil || sub.HWIDScope != "" || !f.panel.legacy[2] {
		t.Errorf("same panel: %v, scope %q, legacy %v", err, sub.HWIDScope, f.panel.legacy)
	}
	// Another panel never saw the old id.
	other := "https://other.example/sub/X"
	f.panel.set(other, pasted)
	sub, err = s.Edit(context.Background(), sub.ID, Edit{URL: &other})
	if err != nil || sub.HWIDScope != HWIDPanel || f.panel.legacy[3] {
		t.Errorf("other panel: %v, scope %q, legacy %v", err, sub.HWIDScope, f.panel.legacy)
	}
}

func TestPlainHTTPSubscriptionIsMarked(t *testing.T) {
	f := newFixture(t)
	const plain = "http://panel.example/sub/TOKEN"
	f.panel.set(plain, pasted)
	s := f.open(t)
	sub, err := s.Add(context.Background(), AddRequest{URL: plain})
	if err != nil || !sub.Insecure() {
		t.Errorf("http subscription: %v, insecure %v", err, sub.Insecure())
	}
	if InsecureURL(subURL) || !InsecureURL("HTTP://x") {
		t.Error("InsecureURL")
	}
}

func TestFingerprintsAreCached(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted)
	s := f.open(t)
	sub, _ := s.Add(context.Background(), AddRequest{URL: subURL})
	got := s.Subscriptions()[0]
	fps := got.Fingerprints()
	if len(fps) != 2 || fps[0] != got.Nodes[0].Fingerprint() || &got.Fingerprints()[0] != &fps[0] {
		t.Fatalf("fingerprints %v", fps)
	}
	f.panel.set(subURL, nodeA2+"\n"+nodeB)
	if _, err := s.Refresh(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	got = s.Subscriptions()[0]
	if got.Fingerprints()[0] != got.Nodes[0].Fingerprint() || got.Fingerprints()[0] == fps[0] {
		t.Error("fingerprints of the old nodes kept after a refresh")
	}
}

// Changes made at once all reach the file, the last one last.
func TestConcurrentChangesAreAllSaved(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Add(context.Background(), AddRequest{Content: nodeA, Name: fmt.Sprintf("list %d", i)}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := len(f.open(t).Subscriptions()); got != 20 {
		t.Errorf("%d subscriptions on disk, want 20", got)
	}
}
