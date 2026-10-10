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
	"strings"
	"time"
)

// bash.ws says one country for the VPN server's address, other geolocation
// services often another (bases differ, and hosting ranges move between
// countries). So the leak test asks one more free HTTPS service, with no
// API key, about the exit address, through the same tunnel as bash.ws, and
// the app shows both when they disagree.
//
// Only the exit address goes there, and only when the verdict is sure it is
// the VPN's (see leakTest): the device's own address never does.

// leakGeoTimeout bounds the request to each service.
var leakGeoTimeout = 3 * time.Second

// LeakGeo is where a geolocation service puts an address.
type LeakGeo struct {
	// Source is the service: "ipwho.is".
	Source string `json:"source"`
	// Country is the ISO 3166 code in lower case.
	Country     string `json:"country"`
	CountryName string `json:"country_name,omitempty"`
	City        string `json:"city,omitempty"`
	Org         string `json:"org,omitempty"`
}

// exitGeoSource is a geolocation service: URL has one %s for the address.
type exitGeoSource struct {
	Name  string
	URL   string
	Parse func(body []byte) (LeakGeo, error)
}

// leakGeoSources are tried in turn, the next one when the one before
// fails, is rate-limited or knows no country.
var leakGeoSources = []exitGeoSource{
	// lang=ru: the city comes in Russian, as the app shows it.
	{"ipwho.is", "https://ipwho.is/%s?lang=ru", parseIPWho},
	{"ipinfo.io", "https://ipinfo.io/%s/json", parseIPInfo},
}

// exitGeo is where a second service puts ip, through client (the core's
// SOCKS inbound), or nil when none answers: that is no error to show.
func (s *Service) exitGeo(ctx context.Context, client *http.Client, ip string) *LeakGeo {
	// Only a public address of the VPN's goes out.
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return nil
	}
	sources := s.cfg.leakGeo
	if sources == nil {
		sources = leakGeoSources
	}
	for _, src := range sources {
		if ctx.Err() != nil {
			return nil
		}
		if g, err := src.lookup(ctx, client, addr.Unmap().String()); err == nil {
			return &g
		}
	}
	return nil
}

func (src exitGeoSource) lookup(ctx context.Context, client *http.Client, ip string) (LeakGeo, error) {
	ctx, cancel := context.WithTimeout(ctx, leakGeoTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(src.URL, ip), nil)
	if err != nil {
		return LeakGeo{}, err
	}
	req.Header.Set("User-Agent", "CoreShift")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return LeakGeo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return LeakGeo{}, errors.New(src.Name + ": " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return LeakGeo{}, err
	}
	g, err := src.Parse(body)
	if err != nil {
		return LeakGeo{}, fmt.Errorf("%s: %w", src.Name, err)
	}
	g.Source = src.Name
	return g, nil
}

var errNoCountry = errors.New("no country")

// cleanGeo trims a service's answer and drops one without a country.
func cleanGeo(g LeakGeo) (LeakGeo, error) {
	g.Country = strings.ToLower(strings.TrimSpace(g.Country))
	if len(g.Country) != 2 || g.Country == "xx" {
		return LeakGeo{}, errNoCountry
	}
	g.CountryName = strings.TrimSpace(g.CountryName)
	g.City = strings.TrimSpace(g.City)
	g.Org = strings.TrimSpace(g.Org)
	return g, nil
}

// parseIPWho reads ipwho.is: {"success":true,"country_code":"RU",
// "country":"Russia","city":"Moscow","connection":{"org":"..."}}. Its
// rate limit answers {"success":false,"message":"..."}.
func parseIPWho(body []byte) (LeakGeo, error) {
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
		return LeakGeo{}, err
	}
	if in.Success != nil && !*in.Success {
		return LeakGeo{}, errors.New(strings.TrimSpace("refused " + in.Message))
	}
	org := in.Connection.Org
	if strings.TrimSpace(org) == "" {
		org = in.Connection.ISP
	}
	return cleanGeo(LeakGeo{Country: in.CountryCode, CountryName: in.Country, City: in.City, Org: org})
}

var asnPrefix = regexp.MustCompile(`(?i)^AS\s?\d{1,10}\s+`)

// parseIPInfo reads ipinfo.io: {"city":"Moscow","country":"RU",
// "org":"AS12345 Some Hosting"}. It gives the country as a code only.
func parseIPInfo(body []byte) (LeakGeo, error) {
	var in struct {
		City    string `json:"city"`
		Country string `json:"country"`
		Org     string `json:"org"`
		Bogon   bool   `json:"bogon"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return LeakGeo{}, err
	}
	if in.Bogon {
		return LeakGeo{}, errNoCountry
	}
	return cleanGeo(LeakGeo{Country: in.Country, City: in.City, Org: asnPrefix.ReplaceAllString(strings.TrimSpace(in.Org), "")})
}
