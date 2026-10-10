package ruleset

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// NoCategoryError is a v2ray list without the category asked for.
type NoCategoryError struct{ Name string }

func (e *NoCategoryError) Error() string { return fmt.Sprintf("no category %q in the list", e.Name) }

// EmptyCategoryError is a category of a v2ray list with nothing in it, or
// nothing with the attributes asked for ("google@cn").
type EmptyCategoryError struct{ Name string }

func (e *EmptyCategoryError) Error() string { return fmt.Sprintf("category %q is empty", e.Name) }

// Categories returns the names of the categories of a v2ray list (a
// geosite.dat or a geoip.dat), lower-case, sorted, once each. Only the first
// field of an entry is looked at (v2ray writes the name there): a list of
// tens of megabytes is passed over without reading its names.
func Categories(dat []byte) ([]string, error) {
	if len(dat) > MaxDatSize {
		return nil, errors.New("list too large")
	}
	seen := map[string]bool{}
	err := fields(dat, func(num protowire.Number, typ protowire.Type, v []byte) error {
		if num != 1 || typ != protowire.BytesType {
			return nil
		}
		code, err := entryName(v)
		if err != nil {
			return err
		}
		if code = strings.ToLower(code); code != "" {
			seen[code] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("damaged list: %w", err)
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// entryName is entryCode, quicker for the lists whose entries start with
// the name.
func entryName(entry []byte) (string, error) {
	if num, typ, n := protowire.ConsumeTag(entry); n > 0 && num == 1 && typ == protowire.BytesType {
		if v, m := protowire.ConsumeBytes(entry[n:]); m >= 0 {
			return string(v), nil
		}
	}
	return entryCode(entry)
}

// Where the rule sets of SagerNet are listed: the files of the branch the
// sets are kept on, from GitHub (a few hundred kilobytes). ParseIndex reads
// the answer.
const (
	SagerNetGeositeIndex = "https://api.github.com/repos/SagerNet/sing-geosite/git/trees/rule-set"
	SagerNetGeoIPIndex   = "https://api.github.com/repos/SagerNet/sing-geoip/git/trees/rule-set"
	// MaxIndexSize bounds the listing of a branch.
	MaxIndexSize = 8 << 20
)

// IndexOf returns where the sets of link are listed, if they are (the link
// with {name} is SagerNet's).
func IndexOf(link string) (string, bool) {
	switch link {
	case SagerNetGeosite:
		return SagerNetGeositeIndex, true
	case SagerNetGeoIP:
		return SagerNetGeoIPIndex, true
	}
	return "", false
}

// safeName is a category that can be a part of a file's name and of a link.
var safeName = regexp.MustCompile(`^[a-z0-9][a-z0-9!@._-]*$`)

// SafeName reports whether a category name from a list is one the service
// takes in a file's name.
func SafeName(name string) bool {
	return len(name) <= 128 && safeName.MatchString(name) && !strings.Contains(name, "..")
}

// ParseIndex reads GitHub's listing of a branch of rule sets, files
// "<kind>-<category>.srs", and returns the categories, sorted. "google@cn"
// is a category here: the branch has a file for it.
func ParseIndex(b []byte, kind string) ([]string, error) {
	if len(b) > MaxIndexSize {
		return nil, errors.New("listing too large")
	}
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(b, &tree); err != nil {
		return nil, fmt.Errorf("not a listing: %w", err)
	}
	var out []string
	for _, f := range tree.Tree {
		name, ok := strings.CutPrefix(f.Path, kind+"-")
		if !ok || f.Type != "blob" {
			continue
		}
		if name, ok = strings.CutSuffix(name, ".srs"); ok && SafeName(name) {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no categories in the listing")
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// Normalize is a category name to compare by, whatever its spelling: lower
// case, without "-", "_", ":", "." and spaces, and without a leading
// "category", "geosite" or "geoip". "U_a", "category:ua" and "Category-UA"
// are all "ua". What follows "@" is kept.
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case '-', '_', ':', '.', ' ', '\t':
		default:
			b.WriteRune(r)
		}
	}
	s = b.String()
	for {
		cut := false
		for _, p := range [...]string{"category", "geosite", "geoip"} {
			if t := strings.TrimPrefix(s, p); t != s && t != "" && t[0] != '@' {
				s, cut = t, true
			}
		}
		if !cut {
			return s
		}
	}
}

// Resolution is what a name typed by the user comes to in a list of
// categories.
type Resolution struct {
	// Name is the category to use ("category-ua", "google@cn"); empty when
	// there is none to use.
	Name string
	// Exact: the list has the name as it was typed (case aside).
	Exact bool
	// Ambiguous: several categories are the same name by Normalize, none is
	// the one typed: nothing is guessed.
	Ambiguous []string
	// Suggestions are up to three close names, for a typo; they are never
	// applied.
	Suggestions []string
}

// Resolve finds in names (lower-case, as Categories gives them) the category
// the user means by want. The name as it is wins; else the one name that is
// the same by Normalize; several are Ambiguous; none gets Suggestions by
// edit distance. A list of v2ray has no "@" in its names: "google@cn" is
// "google" and the attribute cn, which is kept as it is.
func Resolve(names []string, want string) Resolution {
	low := strings.ToLower(strings.TrimSpace(want))
	for _, p := range [...]string{"geosite:", "geoip:"} {
		low = strings.TrimPrefix(low, p)
	}
	if slices.Contains(names, low) {
		return Resolution{Name: low, Exact: true}
	}
	if code, attrs, ok := strings.Cut(low, "@"); ok && !slices.ContainsFunc(names, func(n string) bool { return strings.Contains(n, "@") }) {
		r := Resolve(names, code)
		if r.Name != "" {
			r.Name += "@" + attrs
		}
		return r
	}
	n := Normalize(low)
	if n == "" {
		return Resolution{}
	}
	var same []string
	for _, name := range names {
		if Normalize(name) == n {
			same = append(same, name)
		}
	}
	switch len(same) {
	case 0:
		return Resolution{Suggestions: Suggest(names, low, 3)}
	case 1:
		return Resolution{Name: same[0]}
	}
	return Resolution{Ambiguous: same, Suggestions: nil}
}

// Suggest returns up to limit names close to want by edit distance, nearest
// first, for a typo. A name that is want with something added or taken off
// counts as close. Names of an attribute ("x@cn") come after the rest.
func Suggest(names []string, want string, limit int) []string {
	n := Normalize(want)
	if n == "" || limit <= 0 {
		return nil
	}
	maxDist := 1
	switch {
	case len(n) > 8:
		maxDist = 3
	case len(n) > 4:
		maxDist = 2
	}
	type cand struct {
		name string
		dist int
	}
	var found []cand
	for _, name := range names {
		m := Normalize(name)
		if m == "" || m == n {
			continue
		}
		if d := distance(n, m, maxDist); d <= maxDist {
			found = append(found, cand{name, d})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		if x, y := strings.Contains(a.name, "@"), strings.Contains(b.name, "@"); x != y {
			return y
		}
		return a.name < b.name
	})
	out := make([]string, 0, limit)
	for _, c := range found {
		if len(out) == limit {
			break
		}
		out = append(out, c.name)
	}
	return out
}

// Complete returns up to limit names to offer while q is typed: those that
// start with it, then those whose normalized name does, then those that
// have it within.
func Complete(names []string, q string, limit int) []string {
	low := strings.ToLower(strings.TrimSpace(q))
	for _, p := range [...]string{"geosite:", "geoip:"} {
		low = strings.TrimPrefix(low, p)
	}
	n := Normalize(low)
	if n == "" || limit <= 0 {
		return nil
	}
	type cand struct {
		name string
		rank int
	}
	var found []cand
	for _, name := range names {
		m := Normalize(name)
		rank := -1
		switch {
		case strings.HasPrefix(name, low):
			rank = 0
		case strings.HasPrefix(m, n):
			rank = 1
		case len(n) > 1 && strings.Contains(m, n):
			rank = 2
		}
		if rank >= 0 {
			if strings.Contains(name, "@") {
				rank += 3
			}
			found = append(found, cand{name, rank})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if len(a.name) != len(b.name) {
			return len(a.name) < len(b.name)
		}
		return a.name < b.name
	})
	out := make([]string, 0, limit)
	for _, c := range found {
		if len(out) == limit {
			break
		}
		out = append(out, c.name)
	}
	return out
}

// distance is the edit distance between a and b, or more than max when it
// is.
func distance(a, b string, max int) int {
	x, y := []rune(a), []rune(b)
	if d := len(x) - len(y); d > max || -d > max {
		return max + 1
	}
	prev := make([]int, len(y)+1)
	cur := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		cur[0] = i
		low := cur[0]
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			low = min(low, cur[j])
		}
		if low > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(y)]
}
