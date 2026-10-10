package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// bash.ws says one country for the VPN server's address, other geolocation
// services often another (bases differ, and hosting ranges move between
// countries). So the leak test also asks several free HTTPS services with no
// API key, all at once, about the exit address, through the same tunnel as
// bash.ws, and takes the country most of them agree on; the app shows how
// they voted when they split.
//
// Only the exit address goes there, and only when the verdict is sure it is
// the VPN's (see LeakTest): the device's own address never does.
//
// The services are the table leakGeoSources below: to add one, add a row
// and a parser; a service whose answer is not what its parser expects just
// counts as failed.

// leakGeoTimeout bounds the request to each service.
var leakGeoTimeout = 3 * time.Second

// leakGeoBash is what bash.ws is called among the voters.
const leakGeoBash = "bash.ws"

// LeakGeoSource is one service's answer for the exit address.
type LeakGeoSource struct {
	Name string `json:"name"`
	// OK: the service answered with a country. Failed ones are left out of
	// the vote and listed only in the details.
	OK bool `json:"ok"`
	// Country is the ISO 3166 code in lower case.
	Country     string `json:"country,omitempty"`
	CountryName string `json:"country_name,omitempty"`
	City        string `json:"city,omitempty"`
	Org         string `json:"org,omitempty"`
}

// LeakGeoGroup is the services that put the exit in one country.
type LeakGeoGroup struct {
	Country     string   `json:"country"`
	CountryName string   `json:"country_name,omitempty"`
	Sources     []string `json:"sources"`
}

// LeakGeo is where the geolocation services put the exit address.
type LeakGeo struct {
	// Country is the one most services agree on (lower case); City and Org
	// are the most common among those that agree.
	Country     string `json:"country"`
	CountryName string `json:"country_name,omitempty"`
	City        string `json:"city,omitempty"`
	Org         string `json:"org,omitempty"`
	// Votes is how many services put the exit in Country, Total how many
	// answered at all (bash.ws counts as one).
	Votes int `json:"votes"`
	Total int `json:"total"`
	// Tie: another country has as many votes. Then bash.ws's country wins if
	// it is one of them, else the first service's in the table.
	Tie bool `json:"tie,omitempty"`
	// Groups are the countries named, the most voted first.
	Groups []LeakGeoGroup `json:"groups"`
	// Sources are all the services asked, bash.ws first, in the table's
	// order, with those that did not answer.
	Sources []LeakGeoSource `json:"sources"`
}

// exitGeoSource is a geolocation service: URL has one %s for the address.
type exitGeoSource struct {
	Name  string
	URL   string
	Parse func(body []byte) (LeakGeoSource, error)
}

// leakGeoSources are all asked at once. Free, HTTPS, no key; the limits of
// some (ipapi.co) just make them fail now and then, which is fine.
var leakGeoSources = []exitGeoSource{
	// lang=ru: the city comes in Russian, as the app shows it.
	{"ipwho.is", "https://ipwho.is/%s?lang=ru", parseIPWho},
	{"ipinfo.io", "https://ipinfo.io/%s/json", parseIPInfo},
	{"geojs.io", "https://get.geojs.io/v1/ip/geo/%s.json", parseGeoJS},
	{"freeipapi", "https://free.freeipapi.com/api/v1/json/%s", parseFreeIPAPI},
	{"ipapi.co", "https://ipapi.co/%s/json/", parseIPAPICo},
}

// exitGeo is where the services put the exit address of r, asked through
// client (the core's SOCKS inbound), or nil when none but bash.ws answers:
// that is no error to show.
func (s *Service) exitGeo(ctx context.Context, client *http.Client, r *LeakResult) *LeakGeo {
	if r.Exit == nil {
		return nil
	}
	// Only a public address of the VPN's goes out.
	addr, err := netip.ParseAddr(r.Exit.IP)
	if err != nil || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return nil
	}
	sources := s.cfg.leakGeo
	if sources == nil {
		sources = leakGeoSources
	}
	answers := make([]LeakGeoSource, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Go(func() {
			g, err := src.lookup(ctx, client, addr.Unmap().String())
			if err != nil {
				g = LeakGeoSource{Name: src.Name}
			}
			answers[i] = g
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}
	list := []LeakGeoSource{{
		Name: leakGeoBash, OK: r.Exit.Country != "", Country: r.Exit.Country, CountryName: r.Exit.CountryName,
		Org: asnPrefix.ReplaceAllString(strings.TrimSpace(r.Exit.Org), ""),
	}}
	others := 0
	for _, a := range answers {
		if a.OK {
			others++
		}
	}
	if others == 0 {
		return nil
	}
	return consensusGeo(append(list, answers...))
}

// lookup asks the service about ip; a failure of any kind, or an answer the
// parser does not understand, is an error.
func (src exitGeoSource) lookup(ctx context.Context, client *http.Client, ip string) (LeakGeoSource, error) {
	ctx, cancel := context.WithTimeout(ctx, leakGeoTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(src.URL, ip), nil)
	if err != nil {
		return LeakGeoSource{}, err
	}
	req.Header.Set("User-Agent", "CoreShift")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return LeakGeoSource{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return LeakGeoSource{}, errors.New(src.Name + ": " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return LeakGeoSource{}, err
	}
	g, err := safeParse(src.Parse, body)
	if err != nil {
		return LeakGeoSource{}, fmt.Errorf("%s: %w", src.Name, err)
	}
	g.Name, g.OK = src.Name, true
	return g, nil
}

// safeParse runs a parser; one that trips over a strange answer is a failure.
func safeParse(parse func([]byte) (LeakGeoSource, error), body []byte) (g LeakGeoSource, err error) {
	defer func() {
		if r := recover(); r != nil {
			g, err = LeakGeoSource{}, fmt.Errorf("unreadable answer: %v", r)
		}
	}()
	return parse(body)
}

// consensusGeo is the vote of list (bash.ws first, then the table's order).
// Only the answers that are OK count; nil when there are none.
func consensusGeo(list []LeakGeoSource) *LeakGeo {
	type group struct {
		code    string
		members []LeakGeoSource
	}
	var groups []*group
	total := 0
	for _, a := range list {
		if !a.OK || a.Country == "" {
			continue
		}
		total++
		var g *group
		for _, x := range groups {
			if x.code == a.Country {
				g = x
			}
		}
		if g == nil {
			g = &group{code: a.Country}
			groups = append(groups, g)
		}
		g.members = append(g.members, a)
	}
	if total == 0 {
		return nil
	}
	// Stable: of equally voted countries the one named first stays first.
	sort.SliceStable(groups, func(i, j int) bool { return len(groups[i].members) > len(groups[j].members) })
	top := groups[0]
	out := &LeakGeo{
		Country: top.code,
		Votes:   len(top.members),
		Total:   total,
		Tie:     len(groups) > 1 && len(groups[1].members) == len(top.members),
		Sources: list,
	}
	for _, g := range groups {
		grp := LeakGeoGroup{Country: g.code}
		for _, m := range g.members {
			grp.Sources = append(grp.Sources, m.Name)
			if grp.CountryName == "" {
				grp.CountryName = m.CountryName
			}
		}
		out.Groups = append(out.Groups, grp)
	}
	out.CountryName = out.Groups[0].CountryName
	out.City = commonValue(top.members, func(m LeakGeoSource) string { return m.City })
	out.Org = commonValue(top.members, func(m LeakGeoSource) string { return m.Org })
	return out
}

// commonValue is the value most members agree on, ignoring case; of equally
// common ones the first. Empty when none gave one.
func commonValue(members []LeakGeoSource, field func(LeakGeoSource) string) string {
	count := map[string]int{}
	var order []string
	first := map[string]string{}
	for _, m := range members {
		v := strings.TrimSpace(field(m))
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if count[k] == 0 {
			order = append(order, k)
			first[k] = v
		}
		count[k]++
	}
	best := ""
	for _, k := range order {
		if best == "" || count[k] > count[best] {
			best = k
		}
	}
	return first[best]
}

var errNoCountry = errors.New("no country")

var geoCode = regexp.MustCompile(`^[a-z]{2}$`)

// cleanGeo trims an answer and drops one without a country.
func cleanGeo(g LeakGeoSource) (LeakGeoSource, error) {
	g.Country = strings.ToLower(strings.TrimSpace(g.Country))
	if !geoCode.MatchString(g.Country) || g.Country == "xx" {
		return LeakGeoSource{}, errNoCountry
	}
	g.CountryName = strings.TrimSpace(g.CountryName)
	g.City = strings.TrimSpace(g.City)
	g.Org = strings.TrimSpace(g.Org)
	return g, nil
}

var asnPrefix = regexp.MustCompile(`(?i)^AS\s?\d{1,10}\s+`)

// parseIPWho reads ipwho.is: {"success":true,"country_code":"RU",
// "country":"Russia","city":"Moscow","connection":{"org":"..."}}. Its
// limit answers {"success":false,"message":"..."}.
func parseIPWho(body []byte) (LeakGeoSource, error) {
	var in struct {
		Success     *bool  `json:"success"`
		Message     string `json:"message"`
		CountryCode string `json:"country_code"`
		Country     string `json:"country"`
		City        string `json:"city"`
		Connection  struct {
			Org string `json:"org"`
			ISP string `json:"isp"`
		} `json:"connection"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return LeakGeoSource{}, err
	}
	if in.Success != nil && !*in.Success {
		return LeakGeoSource{}, errors.New(strings.TrimSpace("refused " + in.Message))
	}
	org := in.Connection.Org
	if strings.TrimSpace(org) == "" {
		org = in.Connection.ISP
	}
	return cleanGeo(LeakGeoSource{Country: in.CountryCode, CountryName: in.Country, City: in.City, Org: org})
}

// parseIPInfo reads ipinfo.io: {"city":"Moscow","country":"RU",
// "org":"AS12345 Some Hosting"}; the country is a code only.
func parseIPInfo(body []byte) (LeakGeoSource, error) {
	var in struct {
		City    string `json:"city"`
		Country string `json:"country"`
		Org     string `json:"org"`
		Bogon   bool   `json:"bogon"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return LeakGeoSource{}, err
	}
	if in.Bogon {
		return LeakGeoSource{}, errNoCountry
	}
	return cleanGeo(LeakGeoSource{Country: in.Country, City: in.City, Org: asnPrefix.ReplaceAllString(strings.TrimSpace(in.Org), "")})
}

// parseGeoJS reads get.geojs.io: {"country_code":"RU","country":"Russia",
// "city":"Moscow","organization_name":"SOME HOSTING","organization":"AS1 X"}.
func parseGeoJS(body []byte) (LeakGeoSource, error) {
	var in struct {
		CountryCode  string `json:"country_code"`
		Country      string `json:"country"`
		City         string `json:"city"`
		Organization string `json:"organization"`
		OrgName      string `json:"organization_name"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return LeakGeoSource{}, err
	}
	org := in.OrgName
	if strings.TrimSpace(org) == "" {
		org = asnPrefix.ReplaceAllString(strings.TrimSpace(in.Organization), "")
	}
	return cleanGeo(LeakGeoSource{Country: in.CountryCode, CountryName: in.Country, City: in.City, Org: org})
}

// parseFreeIPAPI reads freeipapi.com: {"countryCode":"RU",
// "countryName":"Russia","cityName":"Moscow","asnOrganization":"..."}.
func parseFreeIPAPI(body []byte) (LeakGeoSource, error) {
	var in struct {
		CountryCode string `json:"countryCode"`
		CountryName string `json:"countryName"`
		CityName    string `json:"cityName"`
		ASNOrg      string `json:"asnOrganization"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return LeakGeoSource{}, err
	}
	return cleanGeo(LeakGeoSource{Country: in.CountryCode, CountryName: in.CountryName, City: in.CityName, Org: in.ASNOrg})
}

// parseIPAPICo reads ipapi.co: {"country_code":"RU","country_name":"Russia",
// "city":"Moscow","org":"..."}. Its limit answers {"error":true,"reason":"..."}.
func parseIPAPICo(body []byte) (LeakGeoSource, error) {
	var in struct {
		Error       bool   `json:"error"`
		Reason      string `json:"reason"`
		CountryCode string `json:"country_code"`
		CountryName string `json:"country_name"`
		City        string `json:"city"`
		Org         string `json:"org"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return LeakGeoSource{}, err
	}
	if in.Error {
		return LeakGeoSource{}, errors.New(strings.TrimSpace("refused " + in.Reason))
	}
	return cleanGeo(LeakGeoSource{Country: in.CountryCode, CountryName: in.CountryName, City: in.City, Org: in.Org})
}
