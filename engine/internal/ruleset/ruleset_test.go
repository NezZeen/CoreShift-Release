package ruleset

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

func baseline(t *testing.T, tag string) []byte {
	t.Helper()
	b, _, ok := Baseline(tag)
	if !ok {
		t.Fatalf("no built-in %s", tag)
	}
	return b
}

func decode(t *testing.T, b []byte) option.PlainRuleSet {
	t.Helper()
	rs, err := srs.Read(bytes.NewReader(b), true)
	if err != nil {
		t.Fatal(err)
	}
	return rs.Options
}

func encode(t *testing.T, rs option.PlainRuleSet) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := srs.Write(&buf, rs, C.RuleSetVersion1); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// edit returns the built-in tag with its first rule changed by f.
func edit(t *testing.T, tag string, f func(r *option.DefaultHeadlessRule)) []byte {
	t.Helper()
	rs := decode(t, baseline(t, tag))
	r := rs.Rules[0].DefaultOptions
	r.DomainMatcher, r.IPSet = nil, nil // written from the lists
	f(&r)
	rs.Rules[0].DefaultOptions = r
	return encode(t, rs)
}

func TestBaselines(t *testing.T) {
	m, err := builtIn()
	if err != nil {
		t.Fatal(err)
	}
	if m.Fetched.IsZero() || len(m.Sets) == 0 {
		t.Fatalf("manifest = %+v", m)
	}
	for _, tag := range Tags() {
		b := baseline(t, tag)
		if got := Describe(b); got != m.Sets[tag] {
			t.Errorf("%s: built-in copy %+v, manifest says %+v", tag, got, m.Sets[tag])
		}
		if _, ok := expects[tag]; !ok {
			t.Errorf("%s: nothing it must match", tag)
		}
		if err := Check(tag, b); err != nil {
			t.Errorf("%s: %v", tag, err)
		}
		// Written anew from what it holds: the same set.
		again := encode(t, decode(t, b))
		if err := Check(tag, again, b); err != nil {
			t.Errorf("%s rewritten: %v", tag, err)
		}
	}
	if _, _, ok := Baseline("geosite-nothing"); ok {
		t.Error("a set CoreShift does not carry has a built-in copy")
	}
}

func TestURL(t *testing.T) {
	if got := URL("geoip-ru"); got != "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-ru.srs" {
		t.Error(got)
	}
	if got := URL("geosite-google"); got != "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-google.srs" {
		t.Error(got)
	}
	if got := URL("geosite-ru-blocked"); got != "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geosite/release/geosite.dat" {
		t.Error(got)
	}
	// Every set carried is known, and so checked.
	for _, tag := range Tags() {
		if !slices.Contains(Known(), tag) {
			t.Errorf("%s is carried, not known", tag)
		}
	}
	for _, tag := range []string{"geosite-category-ads-all", "geosite-ru-blocked"} {
		if !slices.Contains(Known(), tag) {
			t.Errorf("%s not known", tag)
		}
	}
}

// The checks of the sets CoreShift has no copy of yet, on made-up ones.
func TestNewSetsChecked(t *testing.T) {
	names := func(n ...string) []byte {
		return encode(t, option.PlainRuleSet{Rules: []option.HeadlessRule{{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultHeadlessRule{DomainSuffix: n}}}})
	}
	if err := Check("geosite-category-ads-all", names("doubleclick.net", "googleadservices.com", "adfox.ru")); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"youtube.com", "vk.com", "yandex.ru", "gosuslugi.ru"} {
		if err := Check("geosite-category-ads-all", names("doubleclick.net", "googleadservices.com", bad)); err == nil {
			t.Errorf("an ad list with %s taken", bad)
		}
	}
	if err := Check("geosite-ru-blocked", names("meduza.io", "linkedin.com")); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"sberbank.ru", "vk.com", "gosuslugi.ru"} {
		if err := Check("geosite-ru-blocked", names("meduza.io", "linkedin.com", bad)); err == nil {
			t.Errorf("a blocked list with %s taken", bad)
		}
	}
}

func TestUpdateAccepted(t *testing.T) {
	ru := baseline(t, "geosite-category-ru")
	// Upstream's usual change: a few names in, a few out.
	upd := edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) {
		r.DomainSuffix = append(slices.Delete(r.DomainSuffix, 20, 25), "new-russian-shop.com", "another-one.net")
		r.Domain = append(r.Domain, "api.new-russian-shop.com")
	})
	if err := Check("geosite-category-ru", upd, ru, ru); err != nil {
		t.Errorf("a usual update was rejected: %v", err)
	}
	// A top-level domain new to the copy on disk, which the built-in copy
	// already has.
	withTLD := edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) { r.DomainSuffix = append(r.DomainSuffix, ".moskva") })
	if err := Check("geosite-category-ru", withTLD, ru, withTLD); err != nil {
		t.Errorf("a wide entry the built-in copy has was rejected: %v", err)
	}
	ip := baseline(t, "geoip-ru")
	updIP := edit(t, "geoip-ru", func(r *option.DefaultHeadlessRule) {
		r.IPCIDR = append(r.IPCIDR[10:], "185.1.2.0/24")
	})
	if err := Check("geoip-ru", updIP, ip); err != nil {
		t.Errorf("a usual address update was rejected: %v", err)
	}
}

func TestRejected(t *testing.T) {
	ru := baseline(t, "geosite-category-ru")
	ip := baseline(t, "geoip-ru")
	google := baseline(t, "geosite-google")
	media := baseline(t, "geosite-category-media-ru-blocked")
	var grown []string // 600 /16 apart: about 39 million addresses more
	for i := range 600 {
		grown = append(grown, fmt.Sprintf("%d.%d.0.0/16", 20+i/128, i%128*2))
	}
	cases := []struct {
		name, tag string
		b         []byte
		refs      [][]byte
		want      string
	}{
		{"error page", "geoip-ru", []byte("<html>blocked</html>"), nil, "not a rule set"},
		{"truncated", "geoip-ru", ip[:len(ip)/2], nil, "damaged"},
		{"header only", "geoip-ru", ip[:4], nil, "damaged"},
		{"one byte off", "geosite-google", func() []byte { b := slices.Clone(google); b[len(b)/2] ^= 0xff; return b }(), nil, ""},
		{"names for addresses", "geoip-ru", ru, nil, "names where addresses"},
		{"addresses for names", "geosite-category-ru", ip, nil, "addresses where names"},
		{"Google without google.com", "geosite-google", edit(t, "geosite-google", func(r *option.DefaultHeadlessRule) {
			r.DomainSuffix = slices.DeleteFunc(r.DomainSuffix, func(s string) bool { return s == "google.com" })
		}), [][]byte{google}, "does not have google.com"},
		{"Russian with meduza.io", "geosite-category-ru", edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) {
			r.DomainSuffix = append(r.DomainSuffix, "meduza.io")
		}), [][]byte{ru}, "has meduza.io"},
		{"Russian with .com", "geosite-category-ru", edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) {
			r.DomainSuffix = append(r.DomainSuffix, ".com")
		}), [][]byte{ru}, "nearly every name"},
		{"Russian with a regular expression for all", "geosite-category-ru", edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) {
			r.DomainRegex = append(r.DomainRegex, `.*`)
		}), [][]byte{ru}, "nearly every name"},
		{"Russian with a new top-level domain", "geosite-category-ru", edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) {
			r.DomainSuffix = append(r.DomainSuffix, "app")
		}), [][]byte{ru}, "new top-level domain app"},
		{"Russian with a new keyword", "geosite-category-ru", edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) {
			r.DomainKeyword = append(r.DomainKeyword, "bank")
		}), [][]byte{ru}, "new keyword bank"},
		{"Russian shrunk", "geosite-category-ru", edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) {
			// What it must have stays, most of the rest goes.
			keep := []string{"vk.com", "yandex.net"}
			r.DomainSuffix = slices.DeleteFunc(r.DomainSuffix, func(s string) bool { return !strings.HasPrefix(s, ".") && !slices.Contains(keep, s) })
			r.Domain = nil
		}), [][]byte{ru}, "down from"},
		{"Russian doubled", "geosite-category-ru", edit(t, "geosite-category-ru", func(r *option.DefaultHeadlessRule) {
			for i := range 2500 {
				r.Domain = append(r.Domain, "site"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26%26))+".example.net")
			}
		}), [][]byte{ru}, "up from"},
		{"addresses shrunk", "geoip-ru", edit(t, "geoip-ru", func(r *option.DefaultHeadlessRule) {
			r.IPCIDR = []string{"77.88.8.0/24", "87.250.250.0/24"}
		}), [][]byte{ip}, "down from"},
		{"shrunk against the built-in copy", "geoip-ru", edit(t, "geoip-ru", func(r *option.DefaultHeadlessRule) {
			r.IPCIDR = append(r.IPCIDR[:len(r.IPCIDR)/3], "77.88.8.0/24", "87.250.250.0/24")
		}), [][]byte{edit(t, "geoip-ru", func(r *option.DefaultHeadlessRule) {
			r.IPCIDR = r.IPCIDR[:len(r.IPCIDR)/2+50]
		}), ip}, "down from"},
		{"addresses with Google's DNS", "geoip-ru", edit(t, "geoip-ru", func(r *option.DefaultHeadlessRule) {
			r.IPCIDR = append(r.IPCIDR, "8.8.8.0/24")
		}), [][]byte{ip}, "has 8.8.8.8"},
		{"addresses with half the Internet", "geoip-ru", edit(t, "geoip-ru", func(r *option.DefaultHeadlessRule) {
			r.IPCIDR = append(r.IPCIDR, "128.0.0.0/1")
		}), nil, "too wide"},
		{"addresses grown", "geoip-ru", edit(t, "geoip-ru", func(r *option.DefaultHeadlessRule) {
			r.IPCIDR = append(r.IPCIDR, grown...)
		}), [][]byte{ip}, "IPv4 addresses"},
		{"a rule that takes everything else", "geosite-category-media-ru-blocked", edit(t, "geosite-category-media-ru-blocked", func(r *option.DefaultHeadlessRule) {
			r.Invert = true
		}), nil, "inverted"},
		{"a rule with ports", "geosite-category-media-ru-blocked", edit(t, "geosite-category-media-ru-blocked", func(r *option.DefaultHeadlessRule) {
			r.Port = []uint16{443}
		}), nil, "other than names"},
		{"a logical rule", "geosite-category-media-ru-blocked", func() []byte {
			rs := decode(t, media)
			r := rs.Rules[0].DefaultOptions
			r.DomainMatcher = nil
			return encode(t, option.PlainRuleSet{Rules: []option.HeadlessRule{{Type: C.RuleTypeLogical, LogicalOptions: option.LogicalHeadlessRule{
				Mode: C.LogicalTypeOr, Rules: []option.HeadlessRule{{Type: C.RuleTypeDefault, DefaultOptions: r}}}}}})
		}(), nil, "logical"},
		{"empty", "geosite-category-media-ru-blocked", encode(t, option.PlainRuleSet{}), nil, "empty"},
	}
	for _, c := range cases {
		err := Check(c.tag, c.b, c.refs...)
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

func TestBrokenReferenceIgnored(t *testing.T) {
	ru := baseline(t, "geosite-category-ru")
	if err := Check("geosite-category-ru", ru, []byte("garbage"), nil); err != nil {
		t.Error(err)
	}
}
