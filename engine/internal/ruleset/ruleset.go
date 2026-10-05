// Package ruleset keeps the rule sets (sing-box .srs) behind the "Russian
// sites direct" preset trustworthy without pinning them, since SagerNet
// updates them every few days.
//
// CoreShift carries a copy of each set, downloaded when it was released
// (coreshift-release rulesets) and so as trustworthy as the release: the
// self-updater installs only what the release key signed. The service
// starts from that copy, so a tampered or unreachable source never leaves
// the preset without its lists, and then takes newer copies from SagerNet
// by itself, but only those Check accepts: a rule set sing-box reads, made
// of names or addresses only, with what the set is for in it and none of
// the probes it must not have, close in size to the copy it replaces and
// to the built-in one, and with no new entries that match whole zones
// (keywords, regular expressions, top-level domains). A rejected copy
// leaves the previous one in place.
//
// What this cannot see: a few names or networks added to a set within those
// bounds. Those would go direct (or, for the proxy sets, through the
// tunnel) until the source is fixed; the built-in copies of the next
// release are checked against the previous ones before they are taken.
package ruleset

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/netip"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// ManifestName is the file next to the built-in copies that lists them.
const ManifestName = "sets.json"

// MaxSize is the largest rule set taken, packed; maxUnpacked bounds what
// it may unpack to.
const (
	MaxSize     = 16 << 20
	maxUnpacked = 64 << 20
)

//go:embed data
var data embed.FS

// Manifest lists the built-in copies: data/sets.json.
type Manifest struct {
	// Fetched is when the copies were downloaded from SagerNet.
	Fetched time.Time       `json:"fetched"`
	Sets    map[string]File `json:"sets"`
}

// File is one built-in copy.
type File struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Describe returns the manifest entry of b.
func Describe(b []byte) File {
	sum := sha256.Sum256(b)
	return File{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b))}
}

var builtIn = sync.OnceValues(func() (Manifest, error) {
	var m Manifest
	b, err := data.ReadFile("data/" + ManifestName)
	if err == nil {
		err = json.Unmarshal(b, &m)
	}
	return m, err
})

// Tags returns the sets CoreShift carries copies of.
func Tags() []string {
	m, err := builtIn()
	if err != nil {
		return nil
	}
	return slices.Sorted(maps.Keys(m.Sets))
}

// Baseline returns the built-in copy of tag and when it was downloaded.
func Baseline(tag string) (b []byte, fetched time.Time, ok bool) {
	m, err := builtIn()
	if err != nil {
		return nil, time.Time{}, false
	}
	if _, listed := m.Sets[tag]; !listed {
		return nil, time.Time{}, false
	}
	b, err = data.ReadFile("data/" + tag + ".srs")
	if err != nil {
		return nil, time.Time{}, false
	}
	return b, m.Fetched, true
}

// URL returns where tag is downloaded from: SagerNet's sing-geoip for the
// geoip sets, sing-geosite for the rest.
func URL(tag string) string {
	repo := "sing-geosite"
	if IsIP(tag) {
		repo = "sing-geoip"
	}
	return "https://raw.githubusercontent.com/SagerNet/" + repo + "/rule-set/" + tag + ".srs"
}

// IsIP reports whether tag is an address (geoip) set.
func IsIP(tag string) bool { return strings.HasPrefix(tag, "geoip-") }

// expect is what a set is for: names or addresses it must match, and ones
// it must not.
type expect struct {
	match, avoid []string
}

// foreign are well-known names outside Russia that a direct set taking
// them would expose to the local network: none of SagerNet's Russian sets
// has them.
var foreign = []string{
	"google.com", "youtube.com", "wikipedia.org", "github.com", "telegram.org", "t.me", "whatsapp.com",
	"signal.org", "proton.me", "torproject.org", "meduza.io", "facebook.com", "instagram.com", "x.com",
	"apple.com", "microsoft.com", "cloudflare.com", "amazon.com",
}

// unrelated are names no set has: one that matches them matches nearly
// everything.
var unrelated = []string{
	"coreshift-probe-q7x2.com", "coreshift-probe-q7x2.org", "coreshift-probe-q7x2.net", "coreshift-probe-q7x2.io",
	"coreshift-probe-q7x2.de", "coreshift-probe-q7x2.info", "coreshift-probe-q7x2.co.uk", "example.com",
}

var expects = map[string]expect{
	"geosite-category-ru": {
		match: []string{"yandex.ru", "ya.ru", "vk.com", "mail.ru", "gosuslugi.ru", "ozon.ru", "yandex.net"},
		avoid: foreign,
	},
	"geoip-ru": {
		// Yandex's DNS and ya.ru; Google's and Cloudflare's DNS, a Google
		// and a GitHub server.
		match: []string{"77.88.8.8", "87.250.250.242"},
		avoid: []string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "142.250.74.46", "140.82.112.3", "104.16.132.229",
			"2001:4860:4860::8888", "2606:4700:4700::1111"},
	},
	"geosite-category-media-ru-blocked": {
		match: []string{"meduza.io", "tvrain.ru", "novayagazeta.ru", "svoboda.org"},
	},
	"geosite-google": {
		match: []string{"google.com", "www.google.com", "google.ru", "youtube.com", "www.youtube.com", "googlevideo.com",
			"ytimg.com", "gstatic.com", "googleapis.com", "android.com"},
		avoid: []string{"yandex.ru", "vk.com", "gosuslugi.ru"},
	},
}

// Bounds of an address set: no network wider than these, no more IPv4
// addresses than maxIPv4 (Russia has about 45 million).
const (
	minBits4 = 8
	minBits6 = 16
	maxIPv4  = 1 << 27
)

// Check reports whether b is an acceptable copy of the set tag. Each of
// refs is an earlier accepted copy of it (the one on disk, the built-in
// one): b must not stray far in size from any of them, and each entry of
// b that matches a whole zone must be in one of them. A ref that does not
// parse is ignored. Without refs, Check tells whether b can be used at all.
func Check(tag string, b []byte, refs ...[]byte) error {
	s, err := parse(b)
	if err != nil {
		return err
	}
	if s.ip != IsIP(tag) {
		if s.ip {
			return errors.New("addresses where names were expected")
		}
		return errors.New("names where addresses were expected")
	}
	if s.ip {
		if s.v4 > maxIPv4 {
			return fmt.Errorf("covers %.0f IPv4 addresses", s.v4)
		}
	} else {
		for _, name := range unrelated {
			if s.has(name) {
				return fmt.Errorf("matches %s, so nearly every name", name)
			}
		}
	}
	e := expects[tag]
	for _, x := range e.match {
		if !s.has(x) {
			return fmt.Errorf("does not have %s", x)
		}
	}
	for _, x := range e.avoid {
		if s.has(x) {
			return fmt.Errorf("has %s", x)
		}
	}
	// Wide entries must come from an earlier copy: one of them is enough,
	// so a copy on disk older than a wide entry the built-in one has does
	// not hold the set back for good.
	var known map[string]bool
	for _, rb := range refs {
		ref, err := parse(rb)
		if err != nil || ref.ip != s.ip {
			continue
		}
		if err := s.near(ref); err != nil {
			return err
		}
		if known == nil {
			known = map[string]bool{}
		}
		maps.Copy(known, ref.wide)
	}
	if known != nil {
		for _, w := range slices.Sorted(maps.Keys(s.wide)) {
			if !known[w] {
				return fmt.Errorf("new %s", w)
			}
		}
	}
	return nil
}

// near reports whether s is close enough in size to ref, an earlier copy:
// no less than half of it, no more than about twice.
func (s *set) near(ref *set) error {
	if s.entries*2 < ref.entries {
		return fmt.Errorf("%d entries, down from %d", s.entries, ref.entries)
	}
	if s.entries > 2*ref.entries+64 {
		return fmt.Errorf("%d entries, up from %d", s.entries, ref.entries)
	}
	if s.ip {
		if s.v4*2 < ref.v4 || s.v4 > ref.v4*1.5+(1<<16) {
			return fmt.Errorf("covers %.0f IPv4 addresses, %.0f before", s.v4, ref.v4)
		}
		if s.v6*2 < ref.v6 || s.v6 > ref.v6*3+(1<<32) {
			return fmt.Errorf("covers %.0f IPv6 /64 networks, %.0f before", s.v6, ref.v6)
		}
	}
	return nil
}

// set is a parsed rule set.
type set struct {
	ip      bool
	rules   []option.DefaultHeadlessRule
	regexps []*regexp.Regexp
	entries int
	// v4 counts the IPv4 addresses covered, v6 the IPv6 /64 networks.
	v4, v6 float64
	// wide are the entries that match whole zones: keywords, regular
	// expressions and one-label suffixes ("ru").
	wide map[string]bool
}

func parse(b []byte) (s *set, err error) {
	if len(b) > MaxSize {
		return nil, errors.New("rule set too large")
	}
	if len(b) < 4 || !bytes.HasPrefix(b, srs.MagicBytes[:]) {
		return nil, errors.New("response is not a rule set")
	}
	// Bound what it unpacks to before sing-box reads it into memory.
	zr, err := zlib.NewReader(bytes.NewReader(b[4:]))
	if err != nil {
		return nil, fmt.Errorf("damaged rule set: %w", err)
	}
	n, err := io.Copy(io.Discard, io.LimitReader(zr, maxUnpacked+1))
	if err != nil {
		return nil, fmt.Errorf("damaged rule set: %w", err)
	}
	if n > maxUnpacked {
		return nil, errors.New("rule set unpacks too large")
	}
	defer func() {
		if r := recover(); r != nil {
			s, err = nil, fmt.Errorf("damaged rule set: %v", r)
		}
	}()
	rs, err := srs.Read(bytes.NewReader(b), true)
	if err != nil {
		return nil, fmt.Errorf("damaged rule set: %w", err)
	}
	if len(rs.Options.Rules) == 0 {
		return nil, errors.New("empty rule set")
	}
	s = &set{wide: map[string]bool{}}
	for i, r := range rs.Options.Rules {
		if r.Type != C.RuleTypeDefault {
			return nil, fmt.Errorf("rule %d is a %s rule", i, r.Type)
		}
		d := r.DefaultOptions
		if d.Invert {
			return nil, fmt.Errorf("rule %d is inverted", i)
		}
		// Only what geosite and geoip sets are made of: names or
		// addresses, nothing that narrows or widens them otherwise.
		rest := d
		rest.Domain, rest.DomainSuffix, rest.DomainKeyword, rest.DomainRegex, rest.DomainMatcher = nil, nil, nil, nil, nil
		rest.IPCIDR, rest.IPSet = nil, nil
		if !reflect.ValueOf(rest).IsZero() {
			return nil, fmt.Errorf("rule %d has conditions other than names or addresses", i)
		}
		names := d.DomainMatcher != nil || len(d.DomainKeyword) > 0 || len(d.DomainRegex) > 0
		addrs := d.IPSet != nil
		switch {
		case names == addrs:
			return nil, fmt.Errorf("rule %d has both names and addresses, or neither", i)
		case i > 0 && addrs != s.ip:
			return nil, errors.New("rules of names and of addresses mixed")
		}
		s.ip = addrs
		if addrs {
			for _, p := range d.IPSet.Prefixes() {
				if p.Addr().Is4() {
					if p.Bits() < minBits4 {
						return nil, fmt.Errorf("network %s is too wide", p)
					}
					s.v4 += math.Ldexp(1, 32-p.Bits())
				} else {
					if p.Bits() < minBits6 {
						return nil, fmt.Errorf("network %s is too wide", p)
					}
					s.v6 += math.Ldexp(1, 64-min(p.Bits(), 64))
				}
				s.entries++
			}
		}
		s.entries += len(d.Domain) + len(d.DomainSuffix) + len(d.DomainKeyword) + len(d.DomainRegex)
		for _, suffix := range d.DomainSuffix {
			if label := strings.TrimPrefix(suffix, "."); !strings.Contains(label, ".") {
				s.wide["top-level domain "+label] = true
			}
		}
		for _, kw := range d.DomainKeyword {
			s.wide["keyword "+kw] = true
		}
		for _, expr := range d.DomainRegex {
			re, err := regexp.Compile(expr)
			if err != nil {
				return nil, fmt.Errorf("rule %d: %w", i, err)
			}
			s.regexps = append(s.regexps, re)
			s.wide["regular expression "+expr] = true
		}
		s.rules = append(s.rules, d)
	}
	return s, nil
}

// has reports whether the set matches x, a name or an address.
func (s *set) has(x string) bool {
	if s.ip {
		a, err := netip.ParseAddr(x)
		if err != nil {
			return false
		}
		for _, r := range s.rules {
			if r.IPSet.Contains(a) {
				return true
			}
		}
		return false
	}
	for _, r := range s.rules {
		if r.DomainMatcher != nil && r.DomainMatcher.Match(x) {
			return true
		}
		for _, kw := range r.DomainKeyword {
			if strings.Contains(x, kw) {
				return true
			}
		}
	}
	for _, re := range s.regexps {
		if re.MatchString(x) {
			return true
		}
	}
	return false
}
