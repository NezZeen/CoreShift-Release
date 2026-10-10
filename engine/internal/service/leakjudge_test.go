package service

import (
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteHost(t *testing.T) {
	for in, want := range map[string]string{
		"1.1.1.1":                           "1.1.1.1",
		"https://94.140.14.14/dns-query":    "94.140.14.14",
		"tls://DNS.AdGuard-DNS.com":         "dns.adguard-dns.com",
		"quic://dns.adguard-dns.com:853":    "dns.adguard-dns.com",
		"8.8.8.8:53":                        "8.8.8.8",
		"[2606:4700:4700::1111]:53":         "2606:4700:4700::1111",
		"dns.example":                       "dns.example",
		"h3://dns.google/dns-query":         "dns.google",
		"  udp://9.9.9.9  ":                 "9.9.9.9",
		"":                                  "",
		"2606:4700:4700::1111":              "2606:4700:4700::1111",
		"https://[2001:4860:4860::8888]/dq": "2001:4860:4860::8888",
	} {
		if got := remoteHost(in); got != want {
			t.Errorf("remoteHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProviderOf(t *testing.T) {
	for in, want := range map[string]string{
		"https://94.140.14.14/dns-query":       "AdGuard",
		"94.140.15.15":                         "AdGuard",
		"tls://dns.adguard-dns.com":            "AdGuard",
		"https://1.1.1.1/dns-query":            "Cloudflare",
		"https://cloudflare-dns.com/dns-query": "Cloudflare",
		"8.8.4.4":                              "Google",
		"https://dns.google/dns-query":         "Google",
		"9.9.9.9":                              "Quad9",
		"https://dns.nextdns.io/abc123":        "NextDNS",
		"77.88.8.8":                            "Yandex",
		"203.0.113.9":                          "",
		"https://doh.example.org/dns-query":    "",
		"https://notadguard-dns.com":           "",
		"":                                     "",
	} {
		got := ""
		if p := providerOf(in); p != nil {
			got = p.Name
		}
		if got != want {
			t.Errorf("providerOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLeakJudge(t *testing.T) {
	de := &LeakHost{IP: "203.0.113.5", Country: "de", ASN: 64500}
	ua := &LeakHost{IP: "203.0.113.6", Country: "ua", ASN: 64500}
	home := &IPInfo{IP: "198.51.100.20", Country: "RU"}
	adguard := "https://94.140.14.14/dns-query"
	dataCamp := func(ip, path string) LeakHost {
		return LeakHost{IP: ip, Country: "de", CountryName: "Germany", Org: "DataCamp Limited", ASN: 212238, Path: path}
	}

	for name, c := range map[string]struct {
		r        LeakResult
		judge    leakJudge
		want     string
		roles    []string // of r.DNS
		provider string
	}{
		"the server's resolver": {
			r:     LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "1", Country: "de", Path: "proxy"}}},
			want:  leakOK,
			roles: []string{leakRoleVPN},
		},
		"the ISP's resolver": {
			r:     LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "2", Country: "ru", Path: "system"}}},
			want:  leakFound,
			roles: []string{leakRoleISP},
		},
		"a public resolver in the home country": {
			r:     LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "3", Country: "ru", Org: "Google LLC", Path: "system"}}},
			want:  leakOK,
			roles: []string{leakRoleVPN},
		},
		// The report: AdGuard through the tunnel, its resolvers in Germany
		// and Cloudflare's behind the VPN server, the exit in Ukraine, the
		// device in Russia. Not a leak.
		"the user's DoH provider abroad": {
			r: LeakResult{Exit: ua, Home: home, DNS: []LeakHost{
				dataCamp("5", "system"), dataCamp("6", "system"),
				{IP: "7", Country: "ru", Org: "Cloudflare, Inc.", Path: "proxy"},
			}},
			judge:    leakJudge{remote: adguard},
			want:     leakOK,
			roles:    []string{leakRoleOwn, leakRoleOwn, leakRoleVPN},
			provider: "AdGuard",
		},
		"the same resolvers when the DNS is another provider's": {
			r:        LeakResult{Exit: ua, Home: home, DNS: []LeakHost{dataCamp("5", "system")}},
			judge:    leakJudge{remote: "https://1.1.1.1/dns-query"},
			want:     leakOK,
			roles:    []string{""},
			provider: "Cloudflare",
		},
		"the user's provider is in the home country": {
			r: LeakResult{Exit: de, Home: home, DNS: []LeakHost{
				{IP: "8", Country: "ru", Org: "YANDEX LLC", ASN: 13238, Path: "system"},
			}},
			judge:    leakJudge{remote: "77.88.8.8"},
			want:     leakOK,
			roles:    []string{leakRoleOwn},
			provider: "Yandex",
		},
		"mixed: the user's DNS and the ISP's": {
			r: LeakResult{Exit: ua, Home: home, DNS: []LeakHost{
				dataCamp("5", "system"),
				{IP: "9", Country: "ru", Org: "AS64501 Home ISP", ASN: 64501, Path: "system"},
			}},
			judge:    leakJudge{remote: adguard},
			want:     leakFound,
			roles:    []string{leakRoleOwn, leakRoleISP},
			provider: "AdGuard",
		},
		"an unknown provider in the exit's country": {
			r:     LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "10", Country: "de", Org: "Some Hosting", Path: "system"}}},
			judge: leakJudge{remote: "203.0.113.9"},
			want:  leakOK,
			roles: []string{leakRoleVPN},
		},
		"an unknown provider in a third country is no leak by itself": {
			r:     LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "11", Country: "nl", Org: "Other Hosting", Path: "system"}}},
			judge: leakJudge{remote: "203.0.113.9"},
			want:  leakOK,
			roles: []string{""},
		},
		// Without a look at the device's network the ISP's resolver in the
		// server's country cannot be told from the server's: no verdict on it.
		"a resolver of the device's ISP where the server is": {
			r: LeakResult{Exit: de, Home: &IPInfo{IP: "198.51.100.20", Country: "DE"}, DNS: []LeakHost{
				{IP: "12", Country: "de", Org: "AS64501 Home ISP", ASN: 64501, Path: "system"},
			}},
			want:  leakOK,
			roles: []string{leakRoleVPN},
		},
		"the home country's resolver when the exit is elsewhere, however named": {
			r: LeakResult{Exit: ua, Home: home, DNS: []LeakHost{
				{IP: "13", Country: "ru", Org: "AS64501 Home ISP", ASN: 64501, Path: "system"},
			}},
			want:  leakFound,
			roles: []string{leakRoleISP},
		},
		"the home country's resolver when the exit is there too": {
			r: LeakResult{Exit: &LeakHost{IP: "203.0.113.7", Country: "ru"}, Home: home, DNS: []LeakHost{
				{IP: "14", Country: "ru", Org: "Some Hosting", Path: "system"},
			}},
			want:  leakOK,
			roles: []string{leakRoleVPN},
		},
		"an unknown resolver elsewhere is neutral": {
			r: LeakResult{Exit: ua, Home: home, DNS: []LeakHost{
				{IP: "15", Country: "nl", Org: "Other Hosting", Path: "system"},
				{IP: "16", Org: "Nobody Knows", Path: "system"},
			}},
			want:  leakOK,
			roles: []string{"", ""},
		},
		"the network's DNS wins over where it is": {
			r: LeakResult{Exit: ua, Home: home, DNS: []LeakHost{
				{IP: "192.0.2.1", Country: "ua", Org: "Hosting", Path: "system"},
			}},
			judge: leakJudge{network: netip.MustParseAddr("192.0.2.1")},
			want:  leakFound,
			roles: []string{leakRoleISP},
		},
		"the network's own DNS server": {
			r:     LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "192.0.2.1", Country: "de", Org: "Hosting", Path: "system"}}},
			judge: leakJudge{network: netip.MustParseAddr("192.0.2.1")},
			want:  leakFound,
			roles: []string{leakRoleISP},
		},
		"the user's provider comes before the network's address": {
			r:        LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "8.8.8.8", Country: "de", Org: "Google LLC", Path: "system"}}},
			judge:    leakJudge{remote: "8.8.4.4", network: netip.MustParseAddr("8.8.8.8")},
			want:     leakOK,
			roles:    []string{leakRoleOwn},
			provider: "Google",
		},
		"around the VPN": {
			r:     LeakResult{Exit: &LeakHost{IP: "198.51.100.77", Country: "ru"}, Home: home, DNS: []LeakHost{{IP: "2", Country: "ru"}}},
			want:  leakBypass,
			roles: []string{""},
		},
		"no exit":             {r: LeakResult{Home: home}, want: leakUnknown},
		"no home":             {r: LeakResult{Exit: de, ServerIP: "203.0.113.9"}, want: leakUnknown},
		"no home, the server": {r: LeakResult{Exit: de, ServerIP: "203.0.113.5"}, want: leakOK},
	} {
		c.judge.apply(&c.r)
		if c.r.Verdict != c.want {
			t.Errorf("%s: verdict %s, want %s", name, c.r.Verdict, c.want)
		}
		for i, want := range c.roles {
			if got := c.r.DNS[i].Role; got != want {
				t.Errorf("%s: DNS %d is %q, want %q", name, i, got, want)
			}
		}
		if c.r.Provider != c.provider {
			t.Errorf("%s: provider %q, want %q", name, c.r.Provider, c.provider)
		}
	}
}

// The verdict is made from what bash.ws reported and what the service knows
// itself: it asks nobody about the device's address, so a Service with no
// way to ask judges all the same.
func TestJudgeLeakAsksNobody(t *testing.T) {
	de := &LeakHost{IP: "203.0.113.5", Country: "de", ASN: 64500}
	home := &IPInfo{IP: "198.51.100.20", Country: "DE"}
	s := &Service{}
	s.connOpts.DNS.Remote = "203.0.113.9"
	s.autoDNS = netip.MustParseAddr("192.0.2.1")
	r := LeakResult{Exit: de, Home: home, DNS: []LeakHost{
		{IP: "192.0.2.1", Country: "de", Org: "Hosting", Path: "system"},
		{IP: "1", Country: "de", Org: "AS64501 Home ISP", ASN: 64501, Path: "system"},
	}}
	s.judgeLeak(&r)
	if r.Verdict != leakFound || r.DNS[0].Role != leakRoleISP || r.DNS[1].Role != leakRoleVPN {
		t.Errorf("result = %+v", r)
	}
}

// The device's address goes to no third party for a verdict: no lookup of
// the network it belongs to remains anywhere in the sources.
func TestNoLookupOfTheHomeAddressNetwork(t *testing.T) {
	banned := []string{"rip" + "estat", "stat.rip" + "e.net", "rip" + "eASN", "leak" + "ASN", "home" + "ASN", "home_" + "asn"}
	for _, root := range []string{"../..", "../../../app/lib"} {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || (!strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".dart")) {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			low := strings.ToLower(string(b))
			for _, w := range banned {
				if strings.Contains(low, strings.ToLower(w)) {
					t.Errorf("%s mentions %q", path, w)
				}
			}
			return nil
		})
	}
}

func TestLeakEntryASN(t *testing.T) {
	for _, c := range []struct {
		e    leakEntry
		want uint32
	}{
		{leakEntry{ASN: "AS15169 Google LLC"}, 15169},
		{leakEntry{ASN: "15169"}, 15169},
		{leakEntry{Org: "AS 13335 Cloudflare"}, 13335},
		{leakEntry{ASN: "", Org: "Example"}, 0},
		{leakEntry{ASN: "Hosting"}, 0},
	} {
		if got := c.e.asn(); got != c.want {
			t.Errorf("%+v: %d, want %d", c.e, got, c.want)
		}
	}
}
