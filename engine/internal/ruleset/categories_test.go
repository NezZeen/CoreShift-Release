package ruleset

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// The names are the same whatever their spelling.
func TestNormalizeTable(t *testing.T) {
	for in, want := range map[string]string{
		"ua":                        "ua",
		"UA":                        "ua",
		"U_a":                       "ua",
		"u.a":                       "ua",
		"u a":                       "ua",
		"category:ua":               "ua",
		"Category-UA":               "ua",
		"category_UA":               "ua",
		"geosite:category-ua":       "ua",
		"GeoIP-UA":                  "ua",
		"category-ru":               "ru",
		"ru-blocked":                "rublocked",
		"category-media-ru-blocked": "mediarublocked",
		"google@cn":                 "google@cn",
		"Category-Google@CN":        "google@cn",
		"geolocation-!cn":           "geolocation!cn",
		// Nothing left of a name is not a name: it is kept.
		"category": "category",
		"geoip":    "geoip",
		"":         "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	names := []string{"category-ads-all", "category-ru", "category-ua", "geolocation-!cn", "google", "ru-blocked", "youtube"}
	both := append(slices.Clone(names), "ua")
	slices.Sort(both)
	for _, c := range []struct {
		names     []string
		want      string
		name      string
		exact     bool
		ambiguous []string
	}{
		// The name as it is.
		{names, "youtube", "youtube", true, nil},
		{names, "YouTube", "youtube", true, nil},
		{names, "category-ru", "category-ru", true, nil},
		{names, "geosite:youtube", "youtube", true, nil},
		// Another spelling of the one name.
		{names, "ru", "category-ru", false, nil},
		{names, "RU", "category-ru", false, nil},
		{names, "ua", "category-ua", false, nil},
		{names, "UA", "category-ua", false, nil},
		{names, "U_a", "category-ua", false, nil},
		{names, "category:ua", "category-ua", false, nil},
		{names, "Category-UA", "category-ua", true, nil},
		{names, "ads-all", "category-ads-all", false, nil},
		{names, "ru_blocked", "ru-blocked", false, nil},
		{names, "ru.blocked", "ru-blocked", false, nil},
		{names, "geolocation!cn", "geolocation-!cn", false, nil},
		// The exact name wins over a spelling that fits more than one.
		{both, "ua", "ua", true, nil},
		{both, "UA", "ua", true, nil},
		{both, "category-ua", "category-ua", true, nil},
		// None of them is the name itself: several fit, nothing is guessed.
		{both, "U_a", "", false, []string{"category-ua", "ua"}},
		{both, "category:ua", "", false, []string{"category-ua", "ua"}},
	} {
		r := Resolve(c.names, c.want)
		if r.Name != c.name || r.Exact != c.exact || !slices.Equal(r.Ambiguous, c.ambiguous) {
			t.Errorf("Resolve(%q) = %+v, want %q exact %v ambiguous %v", c.want, r, c.name, c.exact, c.ambiguous)
		}
		if r.Name != "" && len(r.Suggestions) != 0 {
			t.Errorf("Resolve(%q): suggestions with a name: %+v", c.want, r)
		}
	}
}

// An attribute is kept as it is, whatever comes of the category.
func TestResolveAttributes(t *testing.T) {
	dat := []string{"category-ads-all", "category-ru", "google"}
	for in, want := range map[string]string{
		"google@cn":         "google@cn",
		"Google@CN":         "google@cn",
		"category-ru@ads":   "category-ru@ads",
		"ru@ads":            "category-ru@ads",
		"ads_all@ads":       "category-ads-all@ads",
		"geosite:google@cn": "google@cn",
		"google@cn@!ads":    "google@cn@!ads",
	} {
		if r := Resolve(dat, in); r.Name != want {
			t.Errorf("Resolve(%q) = %+v, want %q", in, r, want)
		}
	}
	// A typo before the attribute is a typo.
	if r := Resolve(dat, "googel@cn"); r.Name != "" || !slices.Contains(r.Suggestions, "google") {
		t.Errorf("googel@cn: %+v", r)
	}
	// SagerNet lists a file per attribute: those are names of the list.
	index := []string{"category-ru", "category-ru@ads", "google", "google@cn"}
	if r := Resolve(index, "Google@CN"); r.Name != "google@cn" || !r.Exact {
		t.Errorf("index: %+v", r)
	}
	if r := Resolve(index, "ru@ads"); r.Name != "category-ru@ads" {
		t.Errorf("index, spelling: %+v", r)
	}
	if r := Resolve(index, "google@ru"); r.Name != "" {
		t.Errorf("index, an attribute it has no file for: %+v", r)
	}
}

// A typo is only suggested, never used.
func TestSuggestTypos(t *testing.T) {
	names := []string{"category-ads-all", "category-ru", "category-ua", "ua", "youtube", "google", "google@cn"}
	for _, c := range []struct {
		typed string
		want  []string
	}{
		{"Uua", []string{"category-ua", "ua"}},
		{"uaa", []string{"category-ua", "ua"}},
		{"youtub", []string{"youtube"}},
		{"yotube", []string{"youtube"}},
		{"gogle", []string{"google"}},
		{"zzzzzz", nil},
	} {
		r := Resolve(names, c.typed)
		if r.Name != "" || r.Exact {
			t.Errorf("%q: a typo was used: %+v", c.typed, r)
		}
		got := r.Suggestions
		if len(got) == 0 {
			got = nil
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: suggestions %v, want %v", c.typed, got, c.want)
		}
	}
	// At most three, nearest first.
	many := []string{"abcd1", "abcd2", "abcd3", "abcd4", "abcd", "abc"}
	got := Suggest(many, "abcde", 3)
	if len(got) != 3 || got[0] != "abcd" {
		t.Errorf("suggest %v", got)
	}
}

func TestComplete(t *testing.T) {
	names := []string{"category-ads-all", "category-ru", "category-ua", "ru-blocked", "ua", "youtube", "google@cn"}
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"ru", []string{"ru-blocked", "category-ru"}},
		{"u", []string{"ua", "category-ua"}},
		{"CATEGORY-U", []string{"category-ua", "ua"}},
		{"geosite:you", []string{"youtube"}},
		{"ads", []string{"category-ads-all"}},
		{"", nil},
		{"nothing", nil},
	} {
		got := Complete(names, c.q, 8)
		if len(got) == 0 {
			got = nil
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("Complete(%q) = %v, want %v", c.q, got, c.want)
		}
	}
	if got := Complete(names, "c", 2); len(got) != 2 {
		t.Errorf("limit: %v", got)
	}
}

// The categories come out of a list as it is written, upper case and all,
// whatever their size.
func TestCategories(t *testing.T) {
	dat := list(
		geoSite("RU", datDomain{domainSuffix, "yandex.ru", nil}),
		geoSite("CATEGORY-ADS-ALL", datDomain{domainSuffix, "ads.example", []string{"ads"}}),
		geoSite("Category-RU", datDomain{domainSuffix, "vk.com", nil}),
		geoSite("EMPTY"),
		geoSite("RU", datDomain{domainSuffix, "again.example", nil}),
	)
	got, err := Categories(dat)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"category-ads-all", "category-ru", "empty", "ru"}; !slices.Equal(got, want) {
		t.Errorf("categories %v, want %v", got, want)
	}
	// The same names as the list gives geoip.
	ips, err := Categories(list(geoIP("RU", false, "77.88.0.0/18"), geoIP("PRIVATE", false, "10.0.0.0/8")))
	if err != nil || !slices.Equal(ips, []string{"private", "ru"}) {
		t.Errorf("geoip categories %v, %v", ips, err)
	}
	// A new category is there at once; every one Categories names, FromDat reads.
	for _, name := range got[:2] {
		if _, err := FromDat(dat, false, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Damaged.
	if _, err := Categories(append(slices.Clone(dat), 0x0a, 0xff, 0xff)); err == nil {
		t.Error("a damaged list was read")
	}
}

// A category that is not in the list is told from one that is empty or a
// list that is damaged.
func TestNoCategoryError(t *testing.T) {
	_, err := FromDat(testSites, false, "ua")
	var nc *NoCategoryError
	if !errors.As(err, &nc) || nc.Name != "ua" || !strings.Contains(err.Error(), `no category "ua"`) {
		t.Errorf("error %v", err)
	}
	if _, err := FromDat(testSites, false, "empty"); err == nil || errors.As(err, &nc) {
		t.Errorf("empty: %v", err)
	}
}

const sampleTree = `{"sha":"x","tree":[
 {"path":"README.md","type":"blob"},
 {"path":"geosite-category-ru.srs","type":"blob"},
 {"path":"geosite-category-ru@ads.srs","type":"blob"},
 {"path":"geosite-google.srs","type":"blob"},
 {"path":"geosite-google@cn.srs","type":"blob"},
 {"path":"geosite-bad name.srs","type":"blob"},
 {"path":"geosite-../x.srs","type":"blob"},
 {"path":"geosite-dir.srs","type":"tree"},
 {"path":"geoip-ru.srs","type":"blob"}
],"truncated":false}`

func TestParseIndex(t *testing.T) {
	got, err := ParseIndex([]byte(sampleTree), "geosite")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"category-ru", "category-ru@ads", "google", "google@cn"}; !slices.Equal(got, want) {
		t.Errorf("geosite %v, want %v", got, want)
	}
	if got, _ := ParseIndex([]byte(sampleTree), "geoip"); !slices.Equal(got, []string{"ru"}) {
		t.Errorf("geoip %v", got)
	}
	for _, bad := range []string{`<html>rate limited</html>`, `{"message":"API rate limit exceeded"}`, `{"tree":[]}`} {
		if _, err := ParseIndex([]byte(bad), "geosite"); err == nil {
			t.Errorf("%s: taken", bad)
		}
	}
	if link, ok := IndexOf(SagerNetGeosite); !ok || link != SagerNetGeositeIndex {
		t.Error("the index of the geosite branch")
	}
	if _, ok := IndexOf("https://example.org/{name}.srs"); ok {
		t.Error("an index for a link nobody lists")
	}
}
