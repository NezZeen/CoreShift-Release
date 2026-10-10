// Package tunlayer builds the configuration of the persistent TUN + DNS layer.
//
// The layer is a sing-box instance that owns the TUN interface, answers and
// hijacks all DNS, and forwards traffic to the SOCKS inbound of whichever core
// (xray, sing-box, mihomo) is currently active. Swapping cores therefore never
// tears down the interface, routes or DNS settings, so nothing leaks mid-swap.
//
// Target schema: sing-box 1.12+; validated against 1.14.2 by
// TestSingBoxAcceptsConfig.
package tunlayer

import (
	"encoding/json"
	"net/netip"
)

const (
	DefaultInterface = "coreshift"
	DefaultMTU       = 9000
	DefaultStack     = "mixed"

	tagTun       = "tun-in"
	tagProxy     = "proxy"
	tagDirect    = "direct"
	tagDNSRemote = "remote"
	tagDNSDirect = "direct"
	tagDNSFakeIP = "fakeip"
)

// fakeIPTTL is the TTL of fake addresses, in seconds. The tunnel answers
// every lookup itself, so repeating them is cheap.
const fakeIPTTL = 1

// dnsTimeout bounds every lookup (sing-box's default is 10 s). The remote
// server's connection runs through the proxy and can die without a word:
// the server or a carrier NAT drops it while a phone sleeps, and nothing on
// this side notices. sing-box replaces it only once a query on it times out
// (dns/transport/https.go, and the TCP/TLS/UDP multiplexer), so until then
// every lookup hangs on it. Timing out sooner replaces it sooner, before
// the system resolvers' own retry (Android's and glibc's come after 5 s),
// which then goes out on a fresh connection. A new connection through the
// proxy needs a few round trips to the server, well inside this.
const dnsTimeout = "4s"

var (
	DefaultAddress      = netip.MustParsePrefix("172.19.0.1/30")
	DefaultAddress6     = netip.MustParsePrefix("fdfe:dcba:9876::1/126")
	DefaultFakeIPRange  = netip.MustParsePrefix("198.18.0.0/15")
	DefaultFakeIPRange6 = netip.MustParsePrefix("fc00::/18")
)

// lanRanges always go direct. Listed explicitly instead of ip_is_private so
// the fake-IP ranges can never be caught by it.
var lanRanges = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "224.0.0.0/4",
	"fd00::/8", "fe80::/10", "ff00::/8",
}

// browserDoHDomains are resolvers browsers use for built-in DNS over HTTPS,
// which bypasses our DNS (and fake-IP). With BlockBrowserDoH they are only
// reached through the proxy.
var browserDoHDomains = []string{
	"dns.google", "dns.google.com", "cloudflare-dns.com", "one.one.one.one",
	"dns.quad9.net", "doh.opendns.com", "dns.nextdns.io", "doh.cleanbrowsing.org",
	"dns.adguard-dns.com", "doh.dns.sb", "dns.alidns.com", "doh.pub",
}

// ipv6Probes are the hosts Windows checks IPv6 connectivity with. They have
// only IPv6 addresses, so over an IPv4-only tunnel every check would fail
// with an error in the log; answering them empty gives Windows the same
// verdict, no IPv6, quietly.
var ipv6Probes = []string{"ipv6.msftconnecttest.com", "ipv6.msftncsi.com"}

// firefoxCanary: answering NXDOMAIN tells Firefox not to turn on DoH by default.
const firefoxCanary = "use-application-dns.net"

// Options configures the layer. Zero values fall back to the defaults above.
type Options struct {
	InterfaceName string
	Address       netip.Prefix // IPv4 TUN address
	Address6      netip.Prefix // optional; zero means IPv4-only
	// RefuseIPv6 is IPv6 switched off by the user: the tunnel is IPv4-only,
	// yet IPv6 must not leave by the host's own network either, where it
	// would show the real address (sites reached through a browser's own
	// DoH, WebRTC, addresses apps hold). The TUN still takes all of it,
	// with Address6 (DefaultAddress6 if unset), and refuses it before a
	// connection is made, so apps fall back to IPv4 at once; DNS sent over
	// IPv6 is still answered. The local network stays out (ExcludeLAN).
	// Needs a system that gives the interface IPv6; where it does not,
	// leave it unset: strict_route then makes IPv6 unreachable (sing-tun:
	// an unreachable rule on Linux, a WFP block on Windows). Ignored with
	// Platform, whose TUN blocks a family it has no address of (Android's
	// VpnService).
	RefuseIPv6 bool
	MTU        uint32
	Stack      string // "mixed", "system" or "gvisor"
	// StrictRoute makes sing-box block traffic that tries to bypass the TUN;
	// on Windows this is what stops DNS leaking to physical adapters (WFP).
	StrictRoute bool

	// Upstream is the SOCKS5 inbound of the active core, and UpstreamUser
	// and UpstreamPass the credentials it requires, if any.
	Upstream     netip.AddrPort
	UpstreamUser string
	UpstreamPass string
	// BypassProcesses are core executables (full paths); their own traffic
	// must go direct or it would loop back into the tunnel.
	BypassProcesses []string
	// DirectDNSProcesses (full paths) have their DNS queries answered by the
	// direct resolver with real addresses, while their traffic still goes
	// through the tunnel: the daemon itself, which looks up proxy servers.
	DirectDNSProcesses []string
	// BypassAddresses are proxy server IPs, excluded from TUN routes for the
	// same reason. Needed where process matching is unavailable.
	BypassAddresses []netip.Prefix
	// ExcludeLAN keeps the local network ranges out of the TUN routes, so
	// they take the system's own routes: on Windows a Hyper-V or WSL
	// switch, Docker or a second adapter is reached by its own interface,
	// where the layer's direct outbound would leave by the default one; on
	// Linux the replies to connections made to this machine from the local
	// network (SSH, file sharing, a VM's host) no longer die in the TUN.
	// Those ranges go direct anyway. LANResolvers are carved out of them.
	// On Linux only DNS to them goes into the TUN (service resolver rules).
	// On Android the VpnService gets the same list (see mobile).
	ExcludeLAN bool
	// LANResolvers are the system's resolvers (a home router, WSL's host):
	// with ExcludeLAN they stay routed into the TUN, so DNS sent to them
	// straight, past the DNS guard, is still hijacked instead of leaking.
	// Addresses outside the private ranges, loopback and link-local ones
	// (which need a zone) are ignored.
	LANResolvers []netip.Addr
	// DirectApps are executable names ("qbittorrent.exe") whose traffic goes
	// direct instead of through the proxy, wherever they are installed.
	// Names match case-insensitively.
	DirectApps []string
	// AppFilter and FilterApps are Android's per-app VPN (store.Routing):
	// the VpnService takes them, sing-box never sees them.
	AppFilter  string
	FilterApps []string
	// DirectIPs are addresses and subnets that go direct. They match
	// connections made by address; names are matched by DNS.DirectSuffixes.
	DirectIPs []netip.Prefix

	// ProxyApps, ProxyIPs and DNS.ProxySuffixes always go through the proxy,
	// winning over every direct list except DirectApps.
	ProxyApps []string
	ProxyIPs  []netip.Prefix
	// Selective sends only the proxy lists through the proxy and everything
	// else direct; the direct lists and rule sets then have nothing to do.
	Selective bool

	// Rules are the user's own rules, in both modes, in order: after the
	// user's lists and before the presets (see DNSOptions.DirectSuffixes).
	// Those of address sets (geoip) come after every rule by name, just
	// before DNS.DirectIPRuleSets: an address set needs the address of a
	// name, looked up through the tunnel, as v2ray's IPIfNonMatch does,
	// and a name decided before is not looked up so.
	Rules []Rule

	DNS DNSOptions

	// CacheFile persists fake-IP mappings across restarts; empty disables it.
	CacheFile string

	// Platform: the TUN comes from the platform (Android's VpnService), which
	// keeps the app itself, and so the cores it starts, outside the VPN.
	// Outbound sockets then need no binding to the physical interface, and
	// matching processes is left to the platform: the process lists are
	// ignored, and the stack is gVisor.
	Platform bool

	LogLevel string
}

type DNSOptions struct {
	// Remote resolves everything not routed direct, through the proxy.
	Remote string
	// Direct resolves direct domains and the cores' own lookups, bypassing
	// the proxy. Usually a resolver captured from the system before connecting.
	// Must be an IP address.
	Direct string

	FakeIP       bool
	FakeIPRange  netip.Prefix
	FakeIPRange6 netip.Prefix

	// The rules go in this order, the first that matches deciding: the
	// user's lists (BlockSuffixes, the apps, ProxySuffixes and ProxyIPs,
	// DirectIPs and DirectSuffixes), the user's Rules, then the presets
	// (BlockRuleSets, PinnedSuffixes and PinnedRuleSets, ProxyRuleSets,
	// HomeSuffixes and DirectRuleSets, DirectIPRuleSets), then the default:
	// the proxy, or direct with Selective.
	//
	// DirectSuffixes are the user's: they go direct wherever the site is.
	DirectSuffixes []string
	// BlockRuleSets are domain sets refused (ads), unless the user's lists
	// or Rules let a name through.
	BlockRuleSets []RuleSet
	// PinnedSuffixes and PinnedRuleSets (domain sets) always go through the
	// proxy, unless the user's lists or Rules say otherwise: Google with
	// the Russian preset.
	PinnedSuffixes []string
	PinnedRuleSets []RuleSet
	// HomeSuffixes are a preset's direct names, like DirectRuleSets
	// (geosite) after its proxy sets; both are resolved directly. With
	// HomeCheck they go direct only where the address is in
	// DirectIPRuleSets, and through the proxy otherwise: sites of the
	// country the user is in go direct where they are hosted there, and
	// through the tunnel where they are abroad (behind a foreign CDN or
	// hosting, which the provider may slow down).
	HomeSuffixes []string
	HomeCheck    bool
	// ProxySuffixes are the user's names that go through the proxy, ahead
	// of every list but the block list.
	ProxySuffixes []string
	// BlockSuffixes are answered NXDOMAIN and their connections refused.
	BlockSuffixes []string
	// DirectRuleSets are domain rule sets (geosite) resolved and routed direct.
	DirectRuleSets []RuleSet
	// ProxyRuleSets are domain rule sets resolved through the tunnel and
	// routed through the proxy even when a direct suffix or rule set also
	// matches them: sites blocked where the user is, on domains that go
	// direct otherwise (novayagazeta.ru under "ru").
	ProxyRuleSets []RuleSet
	// DirectIPRuleSets are IP rule sets (geoip) routed direct. Matching them
	// needs the real address, so connections by name are resolved through
	// the remote DNS first; the proxy then gets the address instead of the
	// name.
	DirectIPRuleSets []RuleSet

	// DirectIPv4Only answers direct names with IPv4 addresses only, for hosts
	// without IPv6 of their own: direct traffic cannot use the tunnel's IPv6,
	// and apps would first try addresses they cannot reach. Only matters
	// with Address6; without it everything is IPv4-only anyway.
	DirectIPv4Only bool

	// BlockBrowserDoH keeps browsers' own DNS over HTTPS inside the tunnel:
	// Firefox is told not to turn it on by itself, so it uses the system
	// DNS, which we hijack, and browsers that have it on anyway reach their
	// resolvers only through the proxy. (Refusing the resolvers instead
	// broke browsers set to use nothing else, like Firefox's maximum
	// protection.) The name is kept from then, as is the setting's.
	BlockBrowserDoH bool
	// BlockDoT rejects port 853, e.g. Android Private DNS in automatic mode.
	BlockDoT bool
}

// RuleSet is a binary (.srs) rule set, e.g. geosite-category-ru: a local
// file (Path) or one sing-box downloads itself (URL).
type RuleSet struct {
	Tag  string
	Path string
	URL  string
}

// What a Rule does with what it matches.
const (
	ActionProxy  = "proxy"
	ActionDirect = "direct"
	ActionBlock  = "block"
)

// Rule is one of the user's own rules: what it matches, one of Domains
// (suffixes), IPs or Set, and Action. See Options.Rules for the order.
type Rule struct {
	Action  string
	Domains []string
	IPs     []netip.Prefix
	// Set is a rule set of names (geosite), or of addresses (geoip) with
	// SetIP.
	Set   *RuleSet
	SetIP bool
}

// DNSAddress is the resolver address sing-box assigns to the TUN interface:
// the one right after the interface's own address. Point dnsguard at it.
func DNSAddress(p netip.Prefix) netip.Addr { return p.Addr().Next() }

type obj = map[string]any

// Build renders the sing-box configuration as JSON.
func Build(o Options) ([]byte, error) {
	cfg, err := build(o)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(cfg, "", "  ")
}
