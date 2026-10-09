package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRule(t *testing.T) {
	good := map[Rule]Rule{
		{Match: " GeoSite:Category-Ads-All ", Action: "Block"}: {Match: "geosite:category-ads-all", Action: RuleBlock},
		{Match: "geosite:google@cn", Action: "proxy"}:          {Match: "geosite:google@cn", Action: RuleProxy},
		{Match: "geosite:geolocation-!cn", Action: "direct"}:   {Match: "geosite:geolocation-!cn", Action: RuleDirect},
		{Match: "geoip:RU", Action: "direct"}:                  {Match: "geoip:ru", Action: RuleDirect},
		{Match: "domain:Example.COM", Action: "proxy"}:         {Match: "example.com", Action: RuleProxy},
		{Match: ".bank.example.", Action: "direct"}:            {Match: "bank.example", Action: RuleDirect},
		{Match: "Госуслуги.РФ", Action: "direct"}:              {Match: "xn--c1aapkosapc.xn--p1ai", Action: RuleDirect},
		{Match: " 10.8.0.1 ", Action: "direct"}:                {Match: "10.8.0.1", Action: RuleDirect},
		{Match: "10.8.0.0/16", Action: "block"}:                {Match: "10.8.0.0/16", Action: RuleBlock},
		{Match: "2001:db8::/32", Action: "proxy"}:              {Match: "2001:db8::/32", Action: RuleProxy},
		{Match: "  ", Action: "nothing"}:                       {},
	}
	for in, want := range good {
		got, err := NormalizeRule(in)
		if err != nil || got != want {
			t.Errorf("%+v: %+v, %v; want %+v", in, got, err, want)
		}
	}
	bad := map[Rule]string{
		{Match: "geosite:", Action: "block"}:                           "no category name",
		{Match: "geosite:../../etc", Action: "block"}:                  "may have only",
		{Match: "geosite:a..b", Action: "block"}:                       `".."`,
		{Match: "geosite:-ads", Action: "block"}:                       "may have only",
		{Match: "geosite:ads all", Action: "block"}:                    "may have only",
		{Match: "geosite:google@", Action: "block"}:                    "empty category or attribute",
		{Match: "geosite:google@@cn", Action: "block"}:                 "empty category or attribute",
		{Match: "geoip:ru@cn", Action: "direct"}:                       "no attributes",
		{Match: "geosite:" + strings.Repeat("a", 65), Action: "block"}: "longer than",
		{Match: "geosite:ads", Action: "allow"}:                        "is not an action",
		{Match: "geosite:ads", Action: ""}:                             "is not an action",
		{Match: "bad domain", Action: "block"}:                         "is not a domain",
		{Match: "300.1.1.1", Action: "block"}:                          "is not an address or subnet",
		{Match: "198.18.0.1", Action: "direct"}:                        "overlaps the tunnel",
	}
	for in, want := range bad {
		if _, err := NormalizeRule(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%+v: %v, want %q", in, err, want)
		}
	}
}

func TestRuleParse(t *testing.T) {
	for match, want := range map[string][2]string{
		"geosite:youtube": {MatchGeosite, "youtube"},
		"geoip:ru":        {MatchGeoIP, "ru"},
		"example.com":     {MatchDomain, "example.com"},
		"10.0.0.0/8":      {MatchIP, "10.0.0.0/8"},
		"2001:db8::1":     {MatchIP, "2001:db8::1"},
	} {
		if k, v := (Rule{Match: match}).Parse(); k != want[0] || v != want[1] {
			t.Errorf("%s: %s %s", match, k, v)
		}
	}
}

func TestRoutingRulesSettings(t *testing.T) {
	set := Defaults()
	if !set.Routing.BlockAds || !set.Routing.RussiaAbroad || set.Routing.Geo.Source != GeoSagerNet || set.Routing.Rules == nil {
		t.Fatalf("defaults: %+v", set.Routing)
	}
	set.Routing.Rules = []Rule{
		{Match: "geosite:YouTube", Action: "proxy"},
		{Match: "geosite:youtube", Action: "direct"}, // the first decides anyway
		{Match: "", Action: ""},
		{Match: "geoip:ru", Action: "direct"},
	}
	set.Routing.Geo = GeoSource{Source: " Custom ", GeositeURL: " https://example.org/lists/geosite.dat ", Presets: true}
	got, err := set.normalize()
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{{Match: "geosite:youtube", Action: RuleProxy}, {Match: "geoip:ru", Action: RuleDirect}}
	if fmt.Sprint(got.Routing.Rules) != fmt.Sprint(want) || got.Routing.Geo.Source != GeoCustom || got.Routing.Geo.GeositeURL != "https://example.org/lists/geosite.dat" {
		t.Errorf("normalized: %+v", got.Routing)
	}

	bad := map[string]func(*Settings){
		"routing.geo.source":      func(s *Settings) { s.Routing.Geo.Source = "elsewhere" },
		"https://":                func(s *Settings) { s.Routing.Geo.GeositeURL = "http://example.org/{name}.srs" },
		"more than once":          func(s *Settings) { s.Routing.Geo.GeoIPURL = "https://example.org/{name}/{name}.srs" },
		"spaces":                  func(s *Settings) { s.Routing.Geo.GeoIPURL = "https://example.org/a b.dat" },
		"user name":               func(s *Settings) { s.Routing.Geo.GeositeURL = "https://me:pw@example.org/geosite.dat" },
		"longer than 2048":        func(s *Settings) { s.Routing.Geo.GeositeURL = "https://example.org/" + strings.Repeat("a", 2048) },
		"at most 200 entries":     func(s *Settings) { s.Routing.Rules = manyRules(201, "d%d.example") },
		"at most 64 categories":   func(s *Settings) { s.Routing.Rules = manyRules(65, "geosite:c%d") },
		"routing.rules: \"bad x":  func(s *Settings) { s.Routing.Rules = []Rule{{Match: "bad x", Action: "block"}} },
		"\"allow\" is not an act": func(s *Settings) { s.Routing.Rules = []Rule{{Match: "x.example", Action: "allow"}} },
	}
	for want, mutate := range bad {
		s := Defaults()
		mutate(&s)
		if _, err := s.normalize(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
	// A source chosen keeps the custom links, for coming back to them.
	s := Defaults()
	s.Routing.Geo = GeoSource{Source: GeoRunetFreedom, GeositeURL: "https://example.org/geosite.dat"}
	if got, err := s.normalize(); err != nil || got.Routing.Geo.GeositeURL == "" {
		t.Errorf("custom link dropped: %+v %v", got.Routing.Geo, err)
	}
}

func manyRules(n int, format string) []Rule {
	var out []Rule
	for i := range n {
		out = append(out, Rule{Match: fmt.Sprintf(format, i), Action: RuleBlock})
	}
	return out
}

// Settings saved before the ad block and the check of Russian sites'
// servers existed get them on; the rules round-trip through the file.
func TestRoutingRulesStored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	old := `{"version":1,"settings":{"routing":{"mode":"all","russia_direct":true}}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := st.Settings().Routing
	if !r.BlockAds || !r.RussiaAbroad || r.Geo.Source != GeoSagerNet || len(r.Rules) != 0 {
		t.Errorf("old settings: %+v", r)
	}
	set := st.Settings()
	set.Routing.BlockAds = false
	set.Routing.Rules = []Rule{{Match: "geosite:category-ads-all", Action: RuleBlock}, {Match: "youtube.com", Action: RuleProxy}}
	set.Routing.Geo = GeoSource{Source: GeoRunetFreedom, Presets: true}
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := again.Settings().Routing
	b, _ := json.Marshal(got.Rules)
	if got.BlockAds || got.Geo != set.Routing.Geo || string(b) != `[{"match":"geosite:category-ads-all","action":"block"},{"match":"youtube.com","action":"proxy"}]` {
		t.Errorf("stored: %+v %s", got, b)
	}
}
