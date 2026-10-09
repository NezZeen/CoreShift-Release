package store

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// What a Rule matches, from Rule.Parse.
const (
	MatchGeosite = "geosite"
	MatchGeoIP   = "geoip"
	MatchDomain  = "domain"
	MatchIP      = "ip"
)

const (
	maxUserRules = 200
	// Every category is a file to download and a rule set to load.
	maxCategories   = 64
	maxCategoryName = 64
	maxSourceURL    = 2048
)

// categoryName is a geosite or geoip category as v2ray and SagerNet name
// them: "category-ads-all", "geolocation-!cn", "google@cn" (an attribute).
// It ends up in a file name, so nothing more.
var categoryName = regexp.MustCompile(`^[a-z0-9][a-z0-9!@._-]*$`)

// Parse splits a normalized rule into what it matches and the value:
// (MatchGeosite, "category-ads-all"), (MatchIP, "10.0.0.0/8").
func (r Rule) Parse() (kind, value string) {
	if k, v, ok := strings.Cut(r.Match, ":"); ok && (k == MatchGeosite || k == MatchGeoIP) {
		return k, v
	}
	if _, err := normalizeIP(r.Match); err == nil {
		return MatchIP, r.Match
	}
	return MatchDomain, r.Match
}

// NormalizeRule tidies what the user typed: "GeoSite:Category-Ads-All"
// into "geosite:category-ads-all", "domain:Example.com" into
// "example.com", " 10.8.0.1 " into "10.8.0.1". An empty match gives an
// empty rule.
func NormalizeRule(r Rule) (Rule, error) {
	raw := r.Match
	m := strings.TrimSpace(r.Match)
	r.Action = strings.ToLower(strings.TrimSpace(r.Action))
	if m == "" {
		return Rule{}, nil
	}
	var err error
	prefix, rest, _ := strings.Cut(m, ":")
	switch strings.ToLower(prefix) {
	case MatchGeosite, MatchGeoIP:
		kind := strings.ToLower(prefix)
		name := strings.ToLower(strings.TrimSpace(rest))
		if err := checkCategory(kind, name); err != nil {
			return Rule{}, fmt.Errorf("%q: %w", raw, err)
		}
		m = kind + ":" + name
	case "domain":
		m, err = normalizeDomain(rest)
	default:
		if strings.ContainsAny(m, "/:") || strings.Trim(m, "0123456789.") == "" {
			m, err = normalizeIP(m)
		} else {
			m, err = normalizeDomain(m)
		}
	}
	if err != nil {
		return Rule{}, err
	}
	if m == "" {
		return Rule{}, nil
	}
	switch r.Action {
	case RuleProxy, RuleDirect, RuleBlock:
	default:
		return Rule{}, fmt.Errorf("%q: %q is not an action (%s, %s or %s)", raw, r.Action, RuleProxy, RuleDirect, RuleBlock)
	}
	r.Match = m
	return r, nil
}

func checkCategory(kind, name string) error {
	if name == "" {
		return errors.New("no category name")
	}
	if len(name) > maxCategoryName {
		return fmt.Errorf("category name is longer than %d characters", maxCategoryName)
	}
	if !categoryName.MatchString(name) {
		return errors.New("category name may have only a-z, 0-9 and !@._-")
	}
	if strings.Contains(name, "..") {
		return errors.New("category name has \"..\"")
	}
	code, attrs, hasAttrs := strings.Cut(name, "@")
	if hasAttrs && kind == MatchGeoIP {
		return errors.New("geoip categories have no attributes")
	}
	if code == "" || hasAttrs && slices.Contains(strings.Split(attrs, "@"), "") {
		return errors.New("empty category or attribute")
	}
	return nil
}

// normalizeRules tidies every rule and drops empty ones and repeats: of
// two rules for the same thing, the first decides anyway.
func normalizeRules(raw []Rule) ([]Rule, error) {
	out := []Rule{}
	var errs []error
	seen := map[string]bool{}
	categories := 0
	for _, r := range raw {
		n, err := NormalizeRule(r)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("routing.rules: %w", err))
		case n.Match != "" && !seen[n.Match]:
			seen[n.Match] = true
			out = append(out, n)
			if k, _ := n.Parse(); k == MatchGeosite || k == MatchGeoIP {
				categories++
			}
		}
	}
	if len(out) > maxUserRules {
		errs = append(errs, fmt.Errorf("routing.rules: at most %d entries", maxUserRules))
	}
	if categories > maxCategories {
		errs = append(errs, fmt.Errorf("routing.rules: at most %d categories", maxCategories))
	}
	return out, errors.Join(errs...)
}

func normalizeGeo(g GeoSource) (GeoSource, error) {
	g.Source = strings.ToLower(strings.TrimSpace(g.Source))
	g.GeositeURL, g.GeoIPURL = strings.TrimSpace(g.GeositeURL), strings.TrimSpace(g.GeoIPURL)
	var errs []error
	switch g.Source {
	case "":
		g.Source = GeoSagerNet
	case GeoSagerNet, GeoRunetFreedom:
	case GeoCustom:
		if g.GeositeURL == "" && g.GeoIPURL == "" {
			errs = append(errs, errors.New("routing.geo: a custom source needs a link"))
		}
	default:
		errs = append(errs, fmt.Errorf("routing.geo.source: %q is none of %q, %q, %q", g.Source, GeoSagerNet, GeoRunetFreedom, GeoCustom))
	}
	for _, f := range []struct{ name, v string }{{"geosite_url", g.GeositeURL}, {"geoip_url", g.GeoIPURL}} {
		if f.v != "" {
			if err := CheckSourceURL(f.v); err != nil {
				errs = append(errs, fmt.Errorf("routing.geo.%s: %w", f.name, err))
			}
		}
	}
	return g, errors.Join(errs...)
}

// CheckSourceURL reports whether raw can be a source of categories: an
// https link, with {name} at most once where a category goes.
func CheckSourceURL(raw string) error {
	if len(raw) > maxSourceURL {
		return fmt.Errorf("link is longer than %d characters", maxSourceURL)
	}
	if strings.Count(raw, "{name}") > 1 {
		return errors.New("link has {name} more than once")
	}
	if strings.ContainsFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return errors.New("link has spaces or control characters")
	}
	u, err := url.Parse(strings.Replace(raw, "{name}", "x", 1))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return errors.New("link must start with https://")
	}
	if u.User != nil {
		return errors.New("link must not have a user name or password")
	}
	return nil
}
