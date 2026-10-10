package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"coreshift/engine/internal/ruleset"
	"coreshift/engine/internal/store"
)

// What the app asks about the bases of categories (geobase.go):
//
//	GET  /v1/geo/status                            both bases and the user's rules that need them
//	GET  /v1/geo/categories?kind=geosite&q=ru      what the name comes to in the base, for the rule editor
//	POST /v1/geo/reload  {"kind": "geosite"}       download a base again now (both without "kind")

// The states of a base.
const (
	BaseNone    = "none"    // the source lists no categories (a link with {name} that is not SagerNet's)
	BaseIdle    = "idle"    // not downloaded yet, nothing under way
	BaseLoading = "loading" // downloading
	BaseLoaded  = "loaded"
	BaseError   = "error" // the download failed and there is no copy
)

// GeoBase is the state of the base of one kind of categories.
type GeoBase struct {
	// Kind is "geosite" or "geoip"; Origin where it comes from: "sagernet",
	// "runetfreedom" or "custom".
	Kind   string `json:"kind"`
	Origin string `json:"origin"`
	State  string `json:"state"`
	// Percent is how much is downloaded while loading, -1 when the size is
	// not known; Bytes and Total the same in bytes.
	Percent  int       `json:"percent,omitempty"`
	Bytes    int64     `json:"bytes,omitempty"`
	Total    int64     `json:"total,omitempty"`
	LoadedAt time.Time `json:"loaded_at,omitzero"`
	// Count is the number of categories of a loaded base.
	Count int    `json:"count,omitempty"`
	Error string `json:"error,omitempty"`
}

// The states of a rule of a category.
const (
	RuleOK        = "ok"        // the category is in the base
	RuleResolved  = "resolved"  // the name is taken for another spelling, Name
	RuleWaiting   = "waiting"   // the base is not loaded yet
	RuleMissing   = "missing"   // the base has no such category
	RuleAmbiguous = "ambiguous" // several categories fit
	RuleError     = "error"     // the base did not download
)

// GeoRule is what a rule of a category comes to in the base.
type GeoRule struct {
	Match       string   `json:"match"`
	State       string   `json:"state"`
	Name        string   `json:"name,omitempty"`
	Candidates  []string `json:"candidates,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// GeoStatus is the bases and the rules of categories.
type GeoStatus struct {
	Bases []GeoBase `json:"bases"`
	Rules []GeoRule `json:"rules"`
}

// GeoCategories answers a name typed in the rule editor.
type GeoCategories struct {
	Kind   string `json:"kind"`
	Q      string `json:"q"`
	Origin string `json:"origin"`
	State  string `json:"state"`
	// Loaded: the base is there and the rest is about it. Listed is false
	// when the source lists nothing: any name is tried.
	Loaded bool `json:"loaded"`
	Listed bool `json:"listed"`
	Count  int  `json:"count,omitempty"`
	// Exact: the base has this very name. Resolved is the category the rule
	// will use (the same for Exact), empty if none. Ambiguous are the
	// categories that fit equally; Suggestions are close names for a typo,
	// never used by themselves. Matches are names to offer while typing.
	Exact       bool     `json:"exact"`
	Resolved    string   `json:"resolved"`
	Ambiguous   []string `json:"ambiguous"`
	Suggestions []string `json:"suggestions"`
	Matches     []string `json:"matches"`
}

func originOf(link string) string {
	switch link {
	case ruleset.SagerNetGeosite, ruleset.SagerNetGeoIP:
		return store.GeoSagerNet
	case ruleset.RunetFreedomDat:
		return store.GeoRunetFreedom
	}
	return store.GeoCustom
}

// baseInfo is the state of the base of src.
func (r *ruleSets) baseInfo(src geoSource, ip bool) GeoBase {
	link := src.of(ip)
	b := GeoBase{Kind: kindOf(ip), Origin: originOf(link)}
	path, k := r.base(link, ip)
	if k == baseNone {
		b.State = BaseNone
		return b
	}
	r.mu.Lock()
	r.lazy()
	j := r.jobs[path]
	p := r.prog[path]
	f, failed := r.failed[path]
	r.mu.Unlock()
	fi, err := os.Stat(path)
	have := err == nil && fi.Mode().IsRegular()
	switch {
	case j != nil:
		b.State, b.Percent = BaseLoading, -1
		if p != nil {
			b.Bytes, b.Total = p.got.Load(), p.total.Load()
			if b.Total > 0 {
				b.Percent = int(min(99, b.Bytes*100/b.Total))
			}
		}
	case have:
		names, at, err := r.namesOf(path, k)
		if err != nil {
			b.State, b.Error = BaseError, err.Error()
			break
		}
		b.State, b.LoadedAt, b.Count = BaseLoaded, at, len(names)
	case failed:
		b.State, b.Error = BaseError, strings.Join(strings.Fields(strings.ReplaceAll(f.err.Error(), "\n", "; ")), " ")
	default:
		b.State = BaseIdle
	}
	return b
}

// ruleState is what the rule with the category name of kind comes to in
// the bases; ok false when nothing can be said (the source lists nothing).
func (r *ruleSets) ruleState(src geoSource, ip bool, rule store.Rule, name string, base GeoBase) (GeoRule, bool) {
	out := GeoRule{Match: rule.Match}
	switch base.State {
	case BaseNone:
		return out, false
	case BaseIdle, BaseLoading:
		out.State = RuleWaiting
		return out, true
	case BaseError:
		// SagerNet's sets are tried by the name as typed without the listing.
		if _, k := r.base(src.of(ip), ip); k == baseIndex {
			return out, false
		}
		out.State = RuleError
		return out, true
	}
	path, k := r.base(src.of(ip), ip)
	names, _, err := r.namesOf(path, k)
	if err != nil {
		out.State = RuleError
		return out, true
	}
	res := ruleset.Resolve(names, name)
	switch {
	case res.Name == name:
		out.State = RuleOK
	case res.Name != "":
		out.State, out.Name = RuleResolved, res.Name
	case len(res.Ambiguous) > 0:
		out.State, out.Candidates = RuleAmbiguous, res.Ambiguous
	default:
		out.State, out.Suggestions = RuleMissing, res.Suggestions
	}
	return out, true
}

// geoProxy is how a base is downloaded: through the core while connected,
// since the source may be blocked where the user is, else directly.
func (s *Service) geoProxy() *url.URL {
	if s.Status().State == Connected {
		return s.proxyURL()
	}
	return nil
}

// GeoStatus tells how the bases of categories are doing and what the
// user's rules of categories come to in them.
func (s *Service) GeoStatus() GeoStatus {
	out := GeoStatus{Bases: []GeoBase{}, Rules: []GeoRule{}}
	st := s.Store()
	if st == nil || s.rules == nil {
		return out
	}
	set := st.Settings()
	src := sourceOf(set.Routing.Geo)
	bases := [2]GeoBase{s.rules.baseInfo(src, false), s.rules.baseInfo(src, true)}
	out.Bases = bases[:]
	for _, rule := range set.Routing.Rules {
		kind, name := rule.Parse()
		if kind != store.MatchGeosite && kind != store.MatchGeoIP {
			continue
		}
		ip := kind == store.MatchGeoIP
		base := bases[0]
		if ip {
			base = bases[1]
		}
		if gr, ok := s.rules.ruleState(src, ip, rule, name, base); ok {
			out.Rules = append(out.Rules, gr)
		}
	}
	return out
}

// GeoCategories says what name comes to in the base of kind ("geosite",
// "geoip"). A base that is not on disk starts downloading.
func (s *Service) GeoCategories(kind, q string) (GeoCategories, error) {
	if kind != store.MatchGeosite && kind != store.MatchGeoIP {
		return GeoCategories{}, fmt.Errorf("kind is geosite or geoip, not %q", kind)
	}
	if len(q) > 128 {
		return GeoCategories{}, errors.New("name is longer than 128 characters")
	}
	st := s.Store()
	if st == nil || s.rules == nil {
		return GeoCategories{}, errors.New("the daemon runs without a store")
	}
	ip := kind == store.MatchGeoIP
	src := sourceOf(st.Settings().Routing.Geo)
	link := src.of(ip)
	base := s.rules.baseInfo(src, ip)
	if base.State == BaseIdle || base.State == BaseError {
		s.rules.prefetch(link, ip, s.geoProxy(), false)
		base = s.rules.baseInfo(src, ip)
	}
	out := GeoCategories{
		Kind: kind, Q: q, Origin: base.Origin, State: base.State, Count: base.Count,
		Loaded: base.State == BaseLoaded, Listed: base.State != BaseNone,
		Ambiguous: []string{}, Suggestions: []string{}, Matches: []string{},
	}
	if !out.Loaded || strings.TrimSpace(q) == "" {
		return out, nil
	}
	path, k := s.rules.base(link, ip)
	names, _, err := s.rules.namesOf(path, k)
	if err != nil {
		return GeoCategories{}, err
	}
	res := ruleset.Resolve(names, q)
	out.Exact, out.Resolved = res.Exact, res.Name
	if len(res.Ambiguous) > 0 {
		out.Ambiguous = res.Ambiguous
	}
	if len(res.Suggestions) > 0 {
		out.Suggestions = res.Suggestions
	}
	if m := ruleset.Complete(names, q, 8); len(m) > 0 {
		out.Matches = m
	}
	return out, nil
}

// GeoReload downloads the base of kind again now ("" for both); one that
// failed is tried at once.
func (s *Service) GeoReload(kind string) error {
	if s.rules == nil || s.Store() == nil {
		return errors.New("the daemon runs without a store")
	}
	src := sourceOf(s.Store().Settings().Routing.Geo)
	var kinds []bool
	switch kind {
	case "":
		kinds = []bool{false, true}
	case store.MatchGeosite:
		kinds = []bool{false}
	case store.MatchGeoIP:
		kinds = []bool{true}
	default:
		return fmt.Errorf("kind is geosite or geoip, not %q", kind)
	}
	for _, ip := range kinds {
		s.rules.prefetch(src.of(ip), ip, s.geoProxy(), true)
	}
	return nil
}

// geoSettingsChanged starts the bases the new settings need: all of them
// when the source changed, those of the kinds of the rules when they did.
// What is downloaded already stays.
func (s *Service) geoSettingsChanged(before, after store.Routing) {
	if s.rules == nil {
		return
	}
	src := sourceOf(after.Geo)
	if before.Geo != after.Geo {
		for _, ip := range []bool{false, true} {
			s.rules.prefetch(src.of(ip), ip, s.geoProxy(), false)
		}
		return
	}
	had := map[string]bool{}
	for _, r := range before.Rules {
		had[r.Match] = true
	}
	for _, r := range after.Rules {
		if kind, _ := r.Parse(); (kind == store.MatchGeosite || kind == store.MatchGeoIP) && !had[r.Match] {
			ip := kind == store.MatchGeoIP
			s.rules.prefetch(src.of(ip), ip, s.geoProxy(), false)
		}
	}
}

func (a *api) routeGeo(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/geo/status", a.geoStatus)
	mux.HandleFunc("GET /v1/geo/categories", a.geoCategories)
	mux.HandleFunc("POST /v1/geo/reload", a.geoReload)
}

func (a *api) geoStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.svc.GeoStatus())
}

func (a *api) geoCategories(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.GeoCategories(r.URL.Query().Get("kind"), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *api) geoReload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := a.svc.GeoReload(req.Kind); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, a.svc.GeoStatus())
}
