package service

import (
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// The verdict of a DNS leak test. A leak is DNS that does not go through
// the tunnel: the lookups reached the resolvers of the user's ISP or of the
// network. That the resolvers are in another country than the VPN server is
// no leak by itself: a public DNS the user chose (AdGuard, Cloudflare, ...)
// has servers all over the world and answers from wherever it is nearest.
//
// Every DNS server bash.ws saw gets a role:
//
//   - own: it belongs to the DNS the user set for the tunnel (dns.remote),
//     recognised by the provider's name in bash.ws's ASN/organisation.
//   - isp: it is the ISP's or the network's: the network's own DNS server,
//     or, when the VPN server is in another country than the device, the
//     device's country.
//   - vpn: it is on the VPN's side: asked by the VPN server, a public
//     resolver, or in the VPN server's country.
//   - none: nothing is known about it; it counts neither way.
//
// The verdict rests on what bash.ws reports and on what the engine knows
// locally (the network's DNS server, the device's country, the DNS set for
// the tunnel). The device's address never goes to a third party for it: no
// lookup of the network the home address belongs to is made.
const (
	leakRoleOwn = "own"
	leakRoleISP = "isp"
	leakRoleVPN = "vpn"
)

// The verdicts of a leak test, as the app tells them (app/lib/state/leak.dart).
const (
	leakOK      = "ok"
	leakFound   = "leak"
	leakBypass  = "bypass"
	leakUnknown = "unknown"
)

// dnsProvider is a public DNS the user can set as the tunnel's DNS.
type dnsProvider struct {
	Name string
	// Hosts are domain suffixes of its DoH/DoT/DoQ names, Nets its addresses.
	Hosts []string
	Nets  []netip.Prefix
	// Orgs are lower-case parts of the ASN/organisation bash.ws gives for the
	// resolvers that ask for names on its behalf: they egress from the
	// provider's own network, or from the hosting it rents.
	Orgs []string
}

func nets(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

var dnsProviders = []dnsProvider{
	// AdGuard's resolvers egress through DataCamp in Germany and elsewhere.
	{"AdGuard", []string{"adguard-dns.com", "adguard.com"}, nets("94.140.14.0/23", "2a10:50c0::/32"), []string{"adguard", "datacamp"}},
	{"Cloudflare", []string{"cloudflare-dns.com", "one.one.one.one", "cloudflare.com"}, nets("1.1.1.0/24", "1.0.0.0/24", "2606:4700:4700::/48"), []string{"cloudflare"}},
	{"Google", []string{"dns.google", "dns.google.com"}, nets("8.8.8.0/24", "8.8.4.0/24", "2001:4860:4860::/48"), []string{"google"}},
	{"Quad9", []string{"quad9.net"}, nets("9.9.9.0/24", "149.112.112.0/24", "2620:fe::/48"), []string{"quad9", "packet clearing house"}},
	{"NextDNS", []string{"nextdns.io"}, nets("45.90.28.0/24", "45.90.30.0/24"), []string{"nextdns"}},
	{"Yandex", []string{"dns.yandex.ru", "dns.yandex.net", "dns.yandex.com"}, nets("77.88.8.0/24", "2a02:6b8::/32"), []string{"yandex"}},
	{"OpenDNS", []string{"opendns.com"}, nets("208.67.222.0/24", "208.67.220.0/24"), []string{"opendns", "cisco"}},
	{"CleanBrowsing", []string{"cleanbrowsing.org"}, nets("185.228.168.0/23"), []string{"cleanbrowsing"}},
	{"Control D", []string{"controld.com"}, nets("76.76.2.0/24", "76.76.10.0/24"), []string{"control d", "controld"}},
	{"Mullvad", []string{"mullvad.net"}, nets("194.242.2.0/24"), []string{"mullvad"}},
}

// leakPublic are public resolvers, which answer from near wherever the
// query left: seeing one means the queries went out through the tunnel, not
// to the ISP.
var leakPublic = []string{"google", "cloudflare", "quad9", "opendns", "cisco", "adguard", "nextdns", "cleanbrowsing", "control d", "mullvad"}

// remoteHost is the host of a DNS server address in the forms the layer
// accepts: "1.1.1.1", "tls://dns.example", "https://1.1.1.1/dns-query",
// "[2606:4700:4700::1111]:53".
func remoteHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			return strings.ToLower(u.Hostname())
		}
		return ""
	}
	if a, err := netip.ParseAddr(raw); err == nil {
		return a.String()
	}
	if ap, err := netip.ParseAddrPort(raw); err == nil {
		return ap.Addr().String()
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(raw)
}

// providerOf is the public DNS the address names, or nil.
func providerOf(remote string) *dnsProvider {
	host := remoteHost(remote)
	if host == "" {
		return nil
	}
	addr, err := netip.ParseAddr(host)
	for i := range dnsProviders {
		p := &dnsProviders[i]
		if err == nil {
			for _, n := range p.Nets {
				if n.Contains(addr.Unmap()) {
					return p
				}
			}
			continue
		}
		for _, h := range p.Hosts {
			if host == h || strings.HasSuffix(host, "."+h) {
				return p
			}
		}
	}
	return nil
}

func (p *dnsProvider) owns(h LeakHost) bool {
	org := strings.ToLower(h.Org)
	for _, o := range p.Orgs {
		if strings.Contains(org, o) {
			return true
		}
	}
	return false
}

// leakJudge is what the verdict knows beside the result itself.
type leakJudge struct {
	// remote is dns.remote of the connection: the DNS the user set for the
	// tunnel.
	remote string
	// network is the DNS server the network gave (invalid when not known).
	network netip.Addr
	// exitCountry is the country the geolocation services agree the VPN
	// server is in, when it is not the one bash.ws gave (lower case).
	exitCountry string
}

// classify says whose a DNS server is; empty when nothing is known.
func (j leakJudge) classify(r *LeakResult, d LeakHost, own *dnsProvider) string {
	exit := r.Exit.Country
	if j.exitCountry != "" {
		exit = j.exitCountry
	}
	home := ""
	if r.Home != nil {
		home = strings.ToLower(r.Home.Country)
	}
	switch {
	case own != nil && own.owns(d):
		return leakRoleOwn
	case j.network.IsValid() && d.IP == j.network.String():
		return leakRoleISP
	case containsAny(strings.ToLower(d.Org), leakPublic):
		return leakRoleVPN
	case d.Country != "" && home != "" && d.Country == home && exit != home:
		return leakRoleISP
	case d.Path == "proxy":
		return leakRoleVPN
	case d.Country != "" && (d.Country == exit || d.Country == r.Exit.Country):
		return leakRoleVPN
	}
	return ""
}

func containsAny(s string, parts []string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// apply gives the result its verdict and the servers their roles.
func (j leakJudge) apply(r *LeakResult) {
	own := providerOf(j.remote)
	r.Provider = ""
	if own != nil {
		r.Provider = own.Name
	}
	for i := range r.DNS {
		r.DNS[i].Role = ""
	}
	if r.Exit == nil {
		r.Verdict = leakUnknown
		return
	}
	if r.bypassed() {
		r.Verdict = leakBypass
		return
	}
	leaked := false
	for i, d := range r.DNS {
		role := j.classify(r, d, own)
		r.DNS[i].Role = role
		leaked = leaked || role == leakRoleISP
	}
	switch {
	case leaked:
		r.Verdict = leakFound
	case r.Exit.IP != r.ServerIP && r.Home == nil:
		// Nothing vouches that the test went through the VPN.
		r.Verdict = leakUnknown
	default:
		r.Verdict = leakOK
	}
}

// bypassed: bash.ws saw the device's own address, or one next to it, as
// providers often hand out neighbouring addresses from one pool.
func (r *LeakResult) bypassed() bool {
	if r.Exit == nil || r.Home == nil || r.Home.IP == "" {
		return false
	}
	if r.Exit.IP == r.Home.IP {
		return true
	}
	e, h := strings.Split(r.Exit.IP, "."), strings.Split(r.Home.IP, ".")
	return len(e) == 4 && len(h) == 4 && strings.Join(e[:3], ".") == strings.Join(h[:3], ".")
}

// ownDNS: the apps' lookups reached the DNS the user set for the tunnel.
func (r LeakResult) ownDNS() bool {
	for _, d := range r.DNS {
		if d.Role == leakRoleOwn {
			return true
		}
	}
	return false
}

// leakJudge is what the service knows of the network and of the DNS set for
// the tunnel, for the verdict.
func (s *Service) leakJudge() leakJudge {
	s.mu.Lock()
	j := leakJudge{remote: s.connOpts.DNS.Remote, network: s.autoDNS}
	s.mu.Unlock()
	if j.remote == "" {
		j.remote = s.Options().DNS.Remote
	}
	return j
}
