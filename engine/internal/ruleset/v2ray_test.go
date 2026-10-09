package ruleset

import (
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// TestFromDatSingBox reads converted sets with a real sing-box. Set
// SINGBOX_BIN to the executable to run it.
func TestFromDatSingBox(t *testing.T) {
	bin := os.Getenv("SINGBOX_BIN")
	if bin == "" {
		t.Skip("SINGBOX_BIN not set")
	}
	ips := list(geoIP("RU", false, "77.88.0.0/18", "2a02:6b8::/32"))
	cases := []struct {
		dat        []byte
		ip         bool
		name       string
		match, not []string
	}{
		{testSites, false, "category-ads-all", []string{"x.ads.example", "tracker.example.org", "a.doubleclick.net", "ad1.example.net"}, []string{"sub.tracker.example.org", "example.com"}},
		{ips, true, "ru", []string{"77.88.8.8", "2a02:6b8::1"}, []string{"8.8.8.8"}},
	}
	for _, c := range cases {
		b, err := FromDat(c.dat, c.ip, c.name)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), c.name+".srs")
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, x := range append(c.match, c.not...) {
			out, err := exec.Command(bin, "rule-set", "match", "-f", "binary", path, x).CombinedOutput()
			if err != nil {
				t.Fatalf("%s %s: %v\n%s", c.name, x, err, out)
			}
			want := !slices.Contains(c.not, x)
			if got := strings.Contains(string(out), "match rules"); got != want {
				t.Errorf("%s: sing-box says %q for %s", c.name, strings.TrimSpace(string(out)), x)
			}
		}
	}
}

// The fixtures are written field by field, as v2ray's tools write them.

type datDomain struct {
	kind  uint64
	value string
	attrs []string
}

func message(fs ...func(b []byte) []byte) []byte {
	var b []byte
	for _, f := range fs {
		b = f(b)
	}
	return b
}

func bytesField(num protowire.Number, v []byte) func([]byte) []byte {
	return func(b []byte) []byte {
		b = protowire.AppendTag(b, num, protowire.BytesType)
		return protowire.AppendBytes(b, v)
	}
}

func varintField(num protowire.Number, v uint64) func([]byte) []byte {
	return func(b []byte) []byte {
		b = protowire.AppendTag(b, num, protowire.VarintType)
		return protowire.AppendVarint(b, v)
	}
}

func geoSite(code string, domains ...datDomain) []byte {
	fs := []func([]byte) []byte{bytesField(1, []byte(code))}
	for _, d := range domains {
		dfs := []func([]byte) []byte{bytesField(2, []byte(d.value))}
		if d.kind != 0 { // proto3 leaves the default out
			dfs = append([]func([]byte) []byte{varintField(1, d.kind)}, dfs...)
		}
		for _, a := range d.attrs {
			dfs = append(dfs, bytesField(3, message(bytesField(1, []byte(a)), varintField(2, 1))))
		}
		fs = append(fs, bytesField(2, message(dfs...)))
	}
	// A field a later version may add is skipped.
	fs = append(fs, varintField(9, 42))
	return message(fs...)
}

func geoIP(code string, reverse bool, cidrs ...string) []byte {
	fs := []func([]byte) []byte{bytesField(1, []byte(code))}
	for _, c := range cidrs {
		p := netip.MustParsePrefix(c)
		// "::ffff:5.255.255.0/120" stays 16 bytes, as some lists have it.
		fs = append(fs, bytesField(2, message(bytesField(1, p.Addr().AsSlice()), varintField(2, uint64(p.Bits())))))
	}
	if reverse {
		fs = append(fs, varintField(3, 1))
	}
	return message(fs...)
}

func list(entries ...[]byte) []byte {
	var fs []func([]byte) []byte
	for _, e := range entries {
		fs = append(fs, bytesField(1, e))
	}
	return message(fs...)
}

var testSites = list(
	geoSite("RU", datDomain{domainSuffix, "yandex.ru", nil}),
	geoSite("CATEGORY-ADS-ALL",
		datDomain{domainSuffix, "ads.example", []string{"ads"}},
		datDomain{domainFull, "tracker.example.org", nil},
		datDomain{domainPlain, "doubleclick", nil},
		datDomain{domainRegex, `^ad[0-9]+\.example\.net$`, []string{"ads"}},
		datDomain{domainRegex, `(?<=lookbehind)`, nil}, // not Go's: left out
	),
	geoSite("EMPTY"),
)

func setOf(t *testing.T, b []byte) *set {
	t.Helper()
	s, err := parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFromDatGeosite(t *testing.T) {
	b, err := FromDat(testSites, false, "category-ads-all")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(b, false); err != nil {
		t.Fatal(err)
	}
	s := setOf(t, b)
	for _, name := range []string{
		"ads.example", "x.ads.example", // domain: and below
		"tracker.example.org",                       // full: exactly
		"stats.doubleclick.net", "ad42.example.net", // keyword, regular expression
	} {
		if !s.has(name) {
			t.Errorf("does not have %s", name)
		}
	}
	for _, name := range []string{"sub.tracker.example.org", "badads.example", "example.net", "yandex.ru"} {
		if s.has(name) {
			t.Errorf("has %s", name)
		}
	}
	if len(s.regexps) != 1 {
		t.Errorf("regular expressions: %d, want the one Go reads", len(s.regexps))
	}

	// An attribute keeps the names that have it.
	b, err = FromDat(testSites, false, "category-ads-all@ads")
	if err != nil {
		t.Fatal(err)
	}
	s = setOf(t, b)
	if !s.has("ads.example") || !s.has("ad7.example.net") || s.has("tracker.example.org") || s.has("doubleclick.com") {
		t.Error("@ads did not keep only the names with the attribute")
	}

	// Any case.
	if _, err := FromDat(testSites, false, "ru"); err != nil {
		t.Error(err)
	}
}

func TestCheckDat(t *testing.T) {
	if err := CheckDat(testSites); err != nil {
		t.Error(err)
	}
	for name, b := range map[string][]byte{
		"html":      []byte("<html>rate limited</html>"),
		"truncated": testSites[:len(testSites)/2],
		"empty":     nil,
		"no name":   list(geoSite("")),
		"rule set":  []byte("SRS\x01\x02\x03"),
	} {
		if err := CheckDat(b); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
}

func TestFromDatErrors(t *testing.T) {
	cases := []struct {
		dat  []byte
		ip   bool
		name string
		want string
	}{
		{testSites, false, "youtube", "no category"},
		{testSites, false, "empty", "is empty"},
		{testSites, false, "ru@nothing", "is empty"},
		{[]byte("<html>not found</html>"), false, "ru", "damaged list"},
		{testSites[:len(testSites)-3], false, "empty", "damaged list"},
		{list(geoIP("RU", true, "77.88.0.0/18")), true, "ru", "inverted"},
		{list(geoIP("RU", false, "77.88.0.0/18")), true, "ru@x", "no attributes"},
	}
	for _, c := range cases {
		_, err := FromDat(c.dat, c.ip, c.name)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

func TestFromDatGeoIP(t *testing.T) {
	dat := list(
		geoIP("PRIVATE", false, "10.0.0.0/8", "224.0.0.0/4", "fc00::/7"),
		geoIP("RU", false, "77.88.0.0/18", "87.250.224.0/19", "2a02:6b8::/32", "::ffff:5.255.255.0/120"),
	)
	b, err := FromDat(dat, true, "ru")
	if err != nil {
		t.Fatal(err)
	}
	s := setOf(t, b)
	for _, a := range []string{"77.88.8.8", "87.250.250.242", "2a02:6b8::1", "5.255.255.5"} {
		if !s.has(a) {
			t.Errorf("does not have %s", a)
		}
	}
	if s.has("8.8.8.8") || s.has("10.1.2.3") {
		t.Error("has addresses of other categories")
	}
	// Networks wider than the presets take are the user's choice here.
	b, err = FromDat(dat, true, "private")
	if err != nil {
		t.Fatal(err)
	}
	if !setOf(t, b).has("239.1.1.1") {
		t.Error("224.0.0.0/4 left out")
	}
	if err := Check("geoip-private", b); err == nil || !strings.Contains(err.Error(), "too wide") {
		t.Errorf("Check took a network too wide for the presets: %v", err)
	}
	// Names asked from an address list, and the other way round.
	if err := Validate(b, false); err == nil {
		t.Error("an address set passed as names")
	}
}
