package service

import (
	"context"
	"errors"
	"net/netip"
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
		"the device's ASN, wherever the server is": {
			r: LeakResult{Exit: de, Home: &IPInfo{IP: "198.51.100.20", Country: "DE"}, DNS: []LeakHost{
				{IP: "12", Country: "de", Org: "AS64501 Home ISP", ASN: 64501, Path: "system"},
			}},
			judge: leakJudge{homeASN: 64501},
			want:  leakFound,
			roles: []string{leakRoleISP},
		},
		"the exit shares the device's ASN": {
			r: LeakResult{Exit: &LeakHost{IP: "203.0.113.5", Country: "de", ASN: 64501}, Home: home, DNS: []LeakHost{
				{IP: "12", Country: "de", Org: "AS64501 Hosting", ASN: 64501, Path: "proxy"},
			}},
			judge: leakJudge{homeASN: 64501},
			want:  leakOK,
			roles: []string{leakRoleVPN},
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

// The device's ASN is looked up only when a server on the apps' path is
// known by no more than its place, and a match then makes it the ISP's.
func TestJudgeLeakLooksTheASNUp(t *testing.T) {
	de := &LeakHost{IP: "203.0.113.5", Country: "de", ASN: 64500}
	home := &IPInfo{IP: "198.51.100.20", Country: "DE"}
	run := func(dns []LeakHost, asn uint32, err error) (LeakResult, int) {
		calls := 0
		s := &Service{cfg: Config{leakASN: func(context.Context, string) (uint32, error) { calls++; return asn, err }}}
		s.connOpts.DNS.Remote = "203.0.113.9"
		r := LeakResult{Exit: de, Home: home, DNS: dns}
		s.judgeLeak(context.Background(), nil, &r)
		return r, calls
	}
	isp := LeakHost{IP: "1", Country: "de", Org: "AS64501 Home ISP", ASN: 64501, Path: "system"}

	r, calls := run([]LeakHost{isp}, 64501, nil)
	if calls != 1 || r.Verdict != leakFound || r.HomeASN != 64501 || r.DNS[0].Role != leakRoleISP {
		t.Errorf("same country as the server: %d lookups, %+v", calls, r)
	}
	if r, calls = run([]LeakHost{isp}, 64999, nil); calls != 1 || r.Verdict != leakOK || r.HomeASN != 64999 {
		t.Errorf("another ASN: %d lookups, %+v", calls, r)
	}
	if r, calls = run([]LeakHost{isp}, 0, errors.New("offline")); calls != 1 || r.Verdict != leakOK || r.HomeASN != 0 {
		t.Errorf("no ASN: %d lookups, %+v", calls, r)
	}
	// What the VPN server's own resolvers asked needs no look.
	proxy := isp
	proxy.Path = "proxy"
	if r, calls = run([]LeakHost{proxy}, 64501, nil); calls != 0 || r.Verdict != leakOK {
		t.Errorf("proxy path: %d lookups, %+v", calls, r)
	}
	// Nor does a server of the DNS the user set.
	s := &Service{cfg: Config{leakASN: func(context.Context, string) (uint32, error) { t.Error("looked up"); return 0, nil }}}
	s.connOpts.DNS.Remote = "https://94.140.14.14/dns-query"
	own := LeakResult{Exit: de, Home: home, DNS: []LeakHost{{IP: "5", Country: "de", Org: "DataCamp Limited", Path: "system"}}}
	s.judgeLeak(context.Background(), nil, &own)
	if own.Verdict != leakOK || own.DNS[0].Role != leakRoleOwn {
		t.Errorf("own = %+v", own)
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
