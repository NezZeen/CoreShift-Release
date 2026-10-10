package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/ruleset"
	"coreshift/engine/internal/store"
)

// skippedBy and resolvedBy are what the journal said of the rules by now.
func (rig *ruleSetsRig) said(line string) map[string]string {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	out := map[string]string{}
	for _, e := range rig.events {
		if e.Line == line {
			out[e.Reason] = e.Error
		}
	}
	return out
}

func (rig *ruleSetsRig) count(line string) int {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	n := 0
	for _, e := range rig.events {
		if e.Line == line {
			n++
		}
	}
	return n
}

// A name is matched with the categories of the base, whatever its spelling:
// one that fits is used and said so once, several are not guessed, a typo is
// not used but offered, a download that failed is another thing.
func TestUserRulesAreMatchedWithTheBase(t *testing.T) {
	src := newSource(t)
	src.set("/geosite.dat", geositeDat(map[string][]string{
		"category-ru":      {"yandex.ru", "vk.com"},
		"category-ua":      {"ukr.net"},
		"ua":               {"gov.ua"},
		"ru-blocked":       {"meduza.io"},
		"category-ads-all": {"doubleclick.net"},
		"google":           {"google.com"},
	}))
	rig := geoRig(t, src)
	o := Options{
		Rules: []store.Rule{
			{Match: "geosite:ru", Action: store.RuleDirect},              // category-ru
			{Match: "geosite:ru_blocked", Action: store.RuleProxy},       // ru-blocked
			{Match: "geosite:u_a", Action: store.RuleBlock},              // ua and category-ua
			{Match: "geosite:ua", Action: store.RuleBlock},               // ua itself
			{Match: "geosite:uua", Action: store.RuleDirect},             // a typo
			{Match: "geosite:category-ads-all", Action: store.RuleBlock}, // as it is
			{Match: "geosite:google@cn", Action: store.RuleProxy},        // an attribute google has no names with
			{Match: "geosite:category:ads:all", Action: store.RuleBlock}, // another spelling of a rule above
		},
		Geo: store.GeoSource{Source: store.GeoCustom, GeositeURL: src.URL + "/geosite.dat"},
	}
	out := rig.routing(context.Background(), o, nil)
	var got []string
	for _, r := range out.rules {
		got = append(got, r.Action+" "+r.Set.Tag)
	}
	want := []string{
		"direct user-geosite-category-ru", "proxy user-geosite-ru-blocked", "block user-geosite-ua",
		"block user-geosite-category-ads-all", "block user-geosite-category-ads-all",
	}
	if !slices.Equal(got, want) {
		t.Errorf("rules %v, want %v", got, want)
	}

	resolved := rig.said("resolved")
	if len(resolved) != 3 || !strings.Contains(resolved["geosite:ru"], "geosite:ru → geosite:category-ru") ||
		!strings.Contains(resolved["geosite:ru_blocked"], "geosite:ru-blocked") || resolved["geosite:category:ads:all"] == "" {
		t.Errorf("resolved: %v", resolved)
	}
	skipped := rig.said("skipped")
	if m := skipped["geosite:u_a"]; !strings.Contains(m, "подходит несколько категорий") || !strings.Contains(m, "«category-ua», «ua»") {
		t.Errorf("ambiguous: %q", m)
	}
	if m := skipped["geosite:uua"]; m != "правило «geosite:uua → напрямую» не работает: в базе своей ссылки нет категории «uua». Похожие: «category-ua», «ua»" {
		t.Errorf("typo: %q", m)
	}
	// google has no name with the attribute cn: the category is there, empty.
	if m := skipped["geosite:google@cn"]; !strings.Contains(m, "в категории «google@cn» базы своей ссылки нет записей") {
		t.Errorf("google@cn: %q", m)
	}
	for match, m := range skipped {
		if strings.Contains(m, "не загрузилась") {
			t.Errorf("%s: a missing category said as a failed download: %q", match, m)
		}
	}

	// Said once: connecting again does not say it again.
	n := rig.count("resolved")
	rig.routing(context.Background(), o, nil)
	if rig.count("resolved") != n {
		t.Errorf("said again: %d, %d", n, rig.count("resolved"))
	}
	// A rule of the exact name says nothing of the kind.
	if _, ok := resolved["geosite:ua"]; ok {
		t.Error("an exact name was said to be resolved")
	}
}

// A category new in the base works at once, with no list of ours.
func TestNewCategoryInTheBaseWorks(t *testing.T) {
	src := newSource(t)
	src.set("/geosite.dat", geositeDat(map[string][]string{"category-ru": {"yandex.ru"}, "brand-new-thing": {"new.example"}}))
	rig := geoRig(t, src)
	o := Options{
		Rules: []store.Rule{{Match: "geosite:brandnewthing", Action: store.RuleProxy}},
		Geo:   store.GeoSource{Source: store.GeoCustom, GeositeURL: src.URL + "/geosite.dat"},
	}
	out := rig.routing(context.Background(), o, nil)
	if len(out.rules) != 1 || out.rules[0].Set.Tag != "user-geosite-brand-new-thing" {
		t.Errorf("rules %+v", out.rules)
	}
}

// What is said when a category cannot be had: not in the base is a thing to
// correct, a download that failed one to wait out.
func TestMissingCategoryIsNotADownloadFailure(t *testing.T) {
	say := func(link string, err error) string {
		rig := newRuleSetsRig(t, time.Now())
		var got Event
		rig.publish = func(e Event) { got = e }
		rig.skipped(store.Rule{Match: "geosite:nothere", Action: store.RuleDirect}, link, err)
		return got.Error
	}
	tmpl := "https://example.org/geosite-{name}.srs"
	notFound := errors.Join(
		errors.New("through the proxy: "+(&httpStatusError{404, "404 Not Found"}).Error()),
		errors.New("directly: server returned 404 Not Found"),
	)
	// A file per category: no file is no category.
	if m := say(tmpl, notFound); m != "правило «geosite:nothere → напрямую» не работает: в базе своей ссылки нет категории «nothere»." {
		t.Errorf("404 of a file per category: %q", m)
	}
	if m := say(ruleset.SagerNetGeosite, notFound); !strings.Contains(m, "в базе SagerNet нет категории «nothere»") || strings.Contains(m, "загрузилась") {
		t.Errorf("SagerNet 404: %q", m)
	}
	// The same 404 of a list is the list not found.
	if m := say(ruleset.RunetFreedomDat, notFound); !strings.Contains(m, "не загрузилась") || !strings.Contains(m, "404") {
		t.Errorf("404 of a list: %q", m)
	}
	// A real download error stays one.
	for _, err := range []error{errors.New("directly: i/o timeout"), errors.New("directly: server returned 503 Service Unavailable"), &net.DNSError{Err: "no such host", Name: "x"}} {
		if m := say(ruleset.SagerNetGeosite, err); !strings.Contains(m, "база из SagerNet не загрузилась") || strings.Contains(m, "нет категории") {
			t.Errorf("%v: %q", err, m)
		}
	}
	if m := say(tmpl, errPending); !strings.Contains(m, "ещё загружается") {
		t.Errorf("pending: %q", m)
	}
}

// A real 404 from a server, end to end.
func TestSagerNetPerFileSourceMissing(t *testing.T) {
	src := newSource(t)
	src.set("/srs/geosite-youtube.srs", namesSet(t, "youtube.com"))
	rig := geoRig(t, src)
	o := Options{
		Rules: []store.Rule{{Match: "geosite:youtube", Action: store.RuleProxy}, {Match: "geosite:nothere", Action: store.RuleBlock}},
		Geo:   store.GeoSource{Source: store.GeoCustom, GeositeURL: src.URL + "/srs/geosite-{name}.srs"},
	}
	if out := rig.routing(context.Background(), o, nil); len(out.rules) != 1 {
		t.Errorf("rules %+v", out.rules)
	}
	if m := rig.said("skipped")["geosite:nothere"]; !strings.Contains(m, "нет категории «nothere»") {
		t.Errorf("said %q", m)
	}
	// A server in trouble is a failed download.
	src.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "oops", http.StatusBadGateway) })
	rig.mu.Lock()
	rig.events = nil
	clear(rig.failed)
	rig.mu.Unlock()
	o.Rules = []store.Rule{{Match: "geosite:other", Action: store.RuleBlock}}
	rig.routing(context.Background(), o, nil)
	if m := rig.said("skipped")["geosite:other"]; !strings.Contains(m, "не загрузилась") || strings.Contains(m, "нет категории") {
		t.Errorf("502: %q", m)
	}
}

// sagerNetRig is a rig whose SagerNet is the function: the listings of its
// branches and its sets by name; anything else is 404.
func sagerNetRig(t *testing.T, index map[string][]string, sets map[string][]byte) (*ruleSetsRig, *sync.Map) {
	t.Helper()
	rig := newRuleSetsRig(t, time.Now())
	rig.publish = func(e Event) {
		rig.mu.Lock()
		rig.events = append(rig.events, e)
		rig.mu.Unlock()
	}
	asked := &sync.Map{}
	rig.fetchGeo = func(_ context.Context, link string, _ *url.URL, _ int64) ([]byte, error) {
		asked.Store(link, true)
		switch link {
		case ruleset.SagerNetGeositeIndex, ruleset.SagerNetGeoIPIndex:
			names, ok := index[link]
			if !ok {
				return nil, &httpStatusError{403, "403 rate limit exceeded"}
			}
			var tree []map[string]string
			for _, n := range names {
				kind := "geosite"
				if link == ruleset.SagerNetGeoIPIndex {
					kind = "geoip"
				}
				tree = append(tree, map[string]string{"path": kind + "-" + n + ".srs", "type": "blob"})
			}
			return json.Marshal(map[string]any{"tree": tree})
		}
		if b, ok := sets[link]; ok {
			return b, nil
		}
		return nil, &httpStatusError{404, "404 Not Found"}
	}
	return rig, asked
}

// SagerNet has a file per category: its listing says what there is, and a
// 404 for a category means it is not there.
func TestSagerNetNamesFromTheListing(t *testing.T) {
	sets := map[string][]byte{
		strings.Replace(ruleset.SagerNetGeosite, "{name}", "category-ua", 1): namesSet(t, "ukr.net"),
		strings.Replace(ruleset.SagerNetGeosite, "{name}", "ua", 1):          namesSet(t, "gov.ua"),
		strings.Replace(ruleset.SagerNetGeosite, "{name}", "tiktok", 1):      namesSet(t, "tiktok.com"),
	}
	rig, _ := sagerNetRig(t, map[string][]string{
		ruleset.SagerNetGeositeIndex: {"category-ru", "category-ua", "tiktok", "youtube", "google", "google@cn"},
		ruleset.SagerNetGeoIPIndex:   {"ru", "ua", "private"},
	}, sets)
	o := Options{
		Rules: []store.Rule{
			{Match: "geosite:ua", Action: store.RuleBlock},      // category-ua, the only one that fits
			{Match: "geosite:tik_tok", Action: store.RuleBlock}, // tiktok
			{Match: "geosite:uua", Action: store.RuleDirect},    // a typo
			{Match: "geosite:youtub", Action: store.RuleDirect}, // a listed name, whose file we do not serve
			{Match: "geoip:ua", Action: store.RuleProxy},        // ua of geoip: no file, listed
		},
		Geo: store.GeoSource{Source: store.GeoSagerNet},
	}
	rig.routing(context.Background(), o, nil)
	skipped := rig.said("skipped")
	if m := skipped["geosite:uua"]; !strings.Contains(m, "нет категории «uua»") || !strings.Contains(m, "Похожие: «category-ua»") {
		t.Errorf("typo: %q", m)
	}
	if m := skipped["geosite:ua"]; m != "" {
		t.Errorf("a name that fits was refused: %q", m)
	}
	if got := rig.said("resolved")["geosite:ua"]; !strings.Contains(got, "geosite:category-ua") {
		t.Errorf("resolved: %v", rig.said("resolved"))
	}
	if got := rig.said("resolved")["geosite:tik_tok"]; got == "" {
		t.Errorf("resolved: %v", rig.said("resolved"))
	}
	// A listed category whose file is not there is told so, not as a failure.
	if m := skipped["geosite:youtub"]; !strings.Contains(m, "нет категории") {
		t.Errorf("youtub: %q", m)
	}
	// The listing is kept: a second connection does not ask for it.
	asked := 0
	rig.fetchGeo = func(_ context.Context, link string, _ *url.URL, _ int64) ([]byte, error) {
		if link == ruleset.SagerNetGeositeIndex {
			asked++
		}
		return nil, &httpStatusError{404, "404 Not Found"}
	}
	rig.routing(context.Background(), o, nil)
	if asked != 0 {
		t.Errorf("the listing was asked for %d times more", asked)
	}
}

// Without the listing (GitHub says no), the name as typed is tried, and a
// 404 is still a category that is not there; a typo cannot be suggested.
func TestSagerNetWithoutTheListing(t *testing.T) {
	rig, _ := sagerNetRig(t, nil, map[string][]byte{
		strings.Replace(ruleset.SagerNetGeosite, "{name}", "tiktok", 1): namesSet(t, "tiktok.com"),
	})
	o := Options{
		Rules: []store.Rule{{Match: "geosite:tiktok", Action: store.RuleBlock}, {Match: "geosite:tiktoc", Action: store.RuleBlock}},
		Geo:   store.GeoSource{Source: store.GeoSagerNet},
	}
	out := rig.routing(context.Background(), o, nil)
	if len(out.rules) != 1 || out.rules[0].Set.Tag != "user-geosite-tiktok" {
		t.Errorf("rules %+v", out.rules)
	}
	m := rig.said("skipped")["geosite:tiktoc"]
	if !strings.Contains(m, "нет категории «tiktoc»") || strings.Contains(m, "Похожие") || strings.Contains(m, "загрузилась") {
		t.Errorf("said %q", m)
	}
}

// What is in the base of a v2ray list, from the file as it is on disk.
func TestBaseCategoriesFromDisk(t *testing.T) {
	src := newSource(t)
	src.set("/geosite.dat", geositeDat(map[string][]string{"category-ru": {"yandex.ru"}, "ua": {"gov.ua"}}))
	rig := geoRig(t, src)
	link := src.URL + "/geosite.dat"
	names, listed, err := rig.listing(context.Background(), link, false, nil)
	if err != nil || !listed || !slices.Equal(names, []string{"category-ru", "ua"}) {
		t.Fatalf("names %v %v %v", names, listed, err)
	}
	// Kept until the file changes.
	path, k := rig.base(link, false)
	again, _, err := rig.namesOf(path, k)
	if err != nil || !slices.Equal(again, names) {
		t.Errorf("again %v %v", again, err)
	}
	b := geositeDat(map[string][]string{"category-ru": {"yandex.ru"}, "ua": {"gov.ua"}, "new": {"n.example"}})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	os.Chtimes(path, future, future)
	if got, _, _ := rig.namesOf(path, k); !slices.Equal(got, []string{"category-ru", "new", "ua"}) {
		t.Errorf("not read again: %v", got)
	}
	// A link that lists nothing says so.
	if _, listed, err := rig.listing(context.Background(), "https://example.org/geosite-{name}.srs", false, nil); listed || err != nil {
		t.Errorf("a link with {name}: %v %v", listed, err)
	}
	// A list that cannot be had is the error, not "no such category".
	_, _, err = rig.listing(context.Background(), src.URL+"/missing.dat", false, nil)
	if err == nil || !isNotFound(err) {
		t.Errorf("a missing list: %v", err)
	}
}
