package store

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/net/idna"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/selfupdate"
	"coreshift/engine/internal/tunlayer"
)

// Settings are the user's preferences. Durations are whole numbers with the
// unit in the name, which keeps the JSON simple for the UI.
type Settings struct {
	// TUN routes all system traffic through the tunnel and guards DNS;
	// without it only the local SOCKS port is served.
	TUN bool `json:"tun"`
	// IPv6 routes IPv6 traffic through the tunnel too. Off, IPv6 is
	// refused while connected (apps use IPv4), never let around the tunnel.
	IPv6 bool `json:"ipv6"`
	// AutoConnect connects the selected node when the daemon starts.
	AutoConnect bool           `json:"auto_connect"`
	Cores       CoreSettings   `json:"cores"`
	DNS         DNSSettings    `json:"dns"`
	Routing     Routing        `json:"routing"`
	Updates     UpdateSettings `json:"updates"`
	AppUpdate   AppUpdate      `json:"app_update"`
	Log         LogSettings    `json:"log"`
}

// LogSettings say how much the journal tells.
type LogSettings struct {
	// Verbose keeps the lines of the cores and the TUN layer that the
	// journal leaves out as harmless, and has the cores tell more (log
	// level info, from the next connection): for finding out what goes
	// wrong. It names the sites the device opens. On by default since
	// 0.9.3.
	Verbose bool `json:"verbose"`
	// Version is logVersion once the defaults of that version applied; a
	// file without it (0.9.2 saved verbose off for everyone) is moved on
	// to them once, after which the user's choice stays.
	Version int `json:"version"`
}

// logVersion is the version of LogSettings' defaults.
const logVersion = 1

// upgrade moves settings saved before logVersion on to its defaults.
func (l *LogSettings) upgrade() {
	if l.Version < logVersion {
		l.Verbose, l.Version = true, logVersion
	}
}

type CoreSettings struct {
	// Priority is the auto-swap order. A core left out is never used.
	Priority []core.Kind `json:"priority"`
	// Mode is "auto" (swap on failure) or "manual" (only Manual).
	Mode   string    `json:"mode"`
	Manual core.Kind `json:"manual,omitempty"`

	HealthURL       string `json:"health_url"`
	HealthIntervalS int    `json:"health_interval_s"`
	// HealthFailures is how many checks in a row must fail before swapping.
	HealthFailures int `json:"health_failures"`
	// MaxLatencyMS counts slower checks as failures; 0 turns it off.
	MaxLatencyMS int `json:"max_latency_ms"`
	// ReturnAfterMin is how long to stay on a backup core before trying the
	// primary again; 0 means never.
	ReturnAfterMin int `json:"return_after_min"`
	// LatencyTest is how the node list measures delay: "ping" (the time of
	// a TCP handshake with the server, ICMP for UDP protocols; quick, no
	// core started) or "proxy" (a request through the node, which also
	// shows whether it works).
	LatencyTest string `json:"latency_test"`
	// Fragment splits the TLS ClientHello to the server to get past DPI
	// (core.Options.Fragment).
	Fragment bool `json:"fragment"`
	// SwitchServer moves to the next server of the subscription that
	// answers when the connected one does not answer at all, while the
	// internet does. A panel's automatic selection switches regardless.
	SwitchServer bool `json:"switch_server"`
}

type DNSSettings struct {
	// Remote resolves names through the tunnel.
	Remote string `json:"remote"`
	// Direct resolves direct names; empty means the system resolver.
	Direct          string `json:"direct"`
	FakeIP          bool   `json:"fake_ip"`
	BlockBrowserDoH bool   `json:"block_browser_doh"`
	BlockDoT        bool   `json:"block_dot"`
	// Strict also disables Windows' smart multi-homed name resolution.
	Strict bool `json:"strict"`
}

type Routing struct {
	// Mode is RouteAll (everything through the VPN except the direct lists)
	// or RouteSelected (only the proxy lists through the VPN).
	Mode string `json:"mode"`
	// RussiaDirect sends Russian sites direct: the .ru, .su and .рф domains,
	// Russian services on other domains and servers located in Russia.
	RussiaDirect bool `json:"russia_direct"`
	// RussiaAbroad sends RussiaDirect's sites direct only where their
	// servers are in Russia (geoip-ru): one hosted abroad goes through the
	// VPN. Off by default: the preset's sites go direct wherever they are.
	RussiaAbroad bool `json:"russia_abroad"`
	// BlockAds refuses ads and trackers (geosite-category-ads-all), in both
	// modes, ahead of every list but the user's Rules. On by default, also
	// for settings saved before it existed.
	BlockAds bool `json:"block_ads"`
	// DirectDomains are domain suffixes that bypass the tunnel, e.g. "ru"
	// or "bank.example". Internationalized names must be in punycode.
	DirectDomains []string `json:"direct_domains"`
	// DirectApps are executable names ("qbittorrent.exe") whose traffic
	// bypasses the tunnel. Only TUN mode can tell apps apart.
	DirectApps []string `json:"direct_apps"`
	// DirectIPs are addresses and subnets ("203.0.113.7", "10.8.0.0/16")
	// that bypass the tunnel: they match connections made by address.
	DirectIPs []string `json:"direct_ips"`

	// The proxy lists always go through the VPN: they win over the direct
	// lists and the Russian preset, and in RouteSelected mode they are the
	// only traffic that does.
	ProxyDomains []string `json:"proxy_domains"`
	ProxyIPs     []string `json:"proxy_ips"`
	ProxyApps    []string `json:"proxy_apps"`

	// BlockDomains are refused outright, in both modes.
	BlockDomains []string `json:"block_domains"`

	// AppFilter decides which Android apps use the VPN at all: AppsAll,
	// AppsExclude (FilterApps go direct, outside the VPN) or AppsOnly (only
	// FilterApps use it). FilterApps are package names. Android applies it
	// to the VPN itself, before any of the rules above; the desktop ignores
	// it, having DirectApps and ProxyApps.
	AppFilter  string   `json:"app_filter"`
	FilterApps []string `json:"filter_apps"`

	// Rules are the user's own rules, in order: the first that matches
	// decides, ahead of the lists above and the Russian preset (only the
	// block list, the apps' lists and the local network come first), in
	// both modes. Like the lists, they work in TUN mode only.
	Rules []Rule `json:"rules"`
	// Geo is where the categories of Rules come from.
	Geo GeoSource `json:"geo"`
}

// Rule is one of the user's rules. Match is "geosite:<category>" (names,
// "category-ads-all", "google@cn"), "geoip:<code>" (addresses, "ru"), a
// domain with its subdomains, or an address or subnet; Action is
// RuleProxy, RuleDirect or RuleBlock.
type Rule struct {
	Match  string `json:"match"`
	Action string `json:"action"`
}

// GeoSource is where geosite and geoip categories come from.
type GeoSource struct {
	// Source is GeoSagerNet, GeoRunetFreedom or GeoCustom.
	Source string `json:"source"`
	// GeositeURL and GeoIPURL are the custom source: an https link with
	// {name} in it, one rule set (.srs) per category, or a link to a
	// v2ray list (geosite.dat, geoip.dat) with them all. Empty, that kind
	// comes from SagerNet. Kept while another source is chosen.
	GeositeURL string `json:"geosite_url"`
	GeoIPURL   string `json:"geoip_url"`
	// Presets takes the lists of the presets (RussiaDirect, BlockAds) from
	// the source too, when it is not SagerNet; the built-in copies stand
	// in for those that cannot be had.
	Presets bool `json:"presets"`
}

type UpdateSettings struct {
	// Auto refreshes subscriptions in the background.
	Auto bool `json:"auto"`
	// IntervalHours applies when the panel does not suggest an interval.
	IntervalHours int `json:"interval_hours"`
	// UserAgent is sent to panels; empty means the engine's default.
	UserAgent string `json:"user_agent"`
}

// AppUpdate is about updates of CoreShift itself.
type AppUpdate struct {
	// Auto installs a new version by itself while the VPN is off.
	Auto bool `json:"auto"`
	// Source is where releases come from, "github:OWNER/REPO" or a folder;
	// empty means the official releases.
	Source string `json:"source"`
}

const (
	ModeAuto   = "auto"
	ModeManual = "manual"

	LatencyPing  = "ping"
	LatencyProxy = "proxy"

	RouteAll      = "all"
	RouteSelected = "selected"

	AppsAll     = "all"
	AppsExclude = "exclude"
	AppsOnly    = "only"

	RuleProxy  = "proxy"
	RuleDirect = "direct"
	RuleBlock  = "block"

	GeoSagerNet     = "sagernet"
	GeoRunetFreedom = "runetfreedom"
	GeoCustom       = "custom"
)

// Defaults returns the settings of a fresh install.
func Defaults() Settings {
	return Settings{
		TUN:  true,
		IPv6: true,
		Log:  LogSettings{Verbose: true, Version: logVersion},
		Cores: CoreSettings{
			Priority:        []core.Kind{core.Xray, core.SingBox, core.Mihomo},
			Mode:            ModeAuto,
			HealthURL:       "http://cp.cloudflare.com/generate_204",
			HealthIntervalS: 15,
			HealthFailures:  3,
			ReturnAfterMin:  10,
			LatencyTest:     LatencyPing,
			SwitchServer:    true,
		},
		DNS: DNSSettings{
			Remote: "https://1.1.1.1/dns-query",
			FakeIP: true,
			Strict: true,
		},
		Routing: Routing{
			Mode:          RouteAll,
			BlockAds:      true,
			DirectDomains: []string{}, DirectApps: []string{}, DirectIPs: []string{},
			ProxyDomains: []string{}, ProxyIPs: []string{}, ProxyApps: []string{},
			AppFilter: AppsAll, FilterApps: []string{},
			BlockDomains: []string{},
			Rules:        []Rule{},
			Geo:          GeoSource{Source: GeoSagerNet},
		},
		Updates:   UpdateSettings{Auto: true, IntervalHours: 12},
		AppUpdate: AppUpdate{Auto: true},
	}
}

// normalize tidies what the user typed and reports the first invalid value.
func (s Settings) normalize() (Settings, error) {
	c := &s.Cores
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if c.Mode == "" {
		c.Mode = ModeAuto
	}
	var errs []error
	seen := map[core.Kind]bool{}
	prio := []core.Kind{}
	for _, k := range c.Priority {
		if _, ok := core.ByKind(k); !ok {
			errs = append(errs, fmt.Errorf("cores.priority: unknown core %q", k))
		} else if !seen[k] {
			seen[k] = true
			prio = append(prio, k)
		}
	}
	c.Priority = prio
	switch c.Mode {
	case ModeAuto:
		if len(c.Priority) == 0 {
			errs = append(errs, errors.New("cores.priority: at least one core is needed"))
		}
	case ModeManual:
		if _, ok := core.ByKind(c.Manual); !ok {
			errs = append(errs, fmt.Errorf("cores.manual: unknown core %q", c.Manual))
		}
	default:
		errs = append(errs, fmt.Errorf("cores.mode: %q is neither %q nor %q", c.Mode, ModeAuto, ModeManual))
	}
	c.LatencyTest = strings.ToLower(strings.TrimSpace(c.LatencyTest))
	switch c.LatencyTest {
	case "":
		c.LatencyTest = LatencyPing
	case LatencyPing, LatencyProxy:
	default:
		errs = append(errs, fmt.Errorf("cores.latency_test: %q is neither %q nor %q", c.LatencyTest, LatencyPing, LatencyProxy))
	}
	c.HealthURL = strings.TrimSpace(c.HealthURL)
	if u, err := url.Parse(c.HealthURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("cores.health_url: %q is not an http(s) URL", c.HealthURL))
	}
	errs = append(errs,
		inRange("cores.health_interval_s", c.HealthIntervalS, 5, 3600),
		inRange("cores.health_failures", c.HealthFailures, 1, 20),
		inRange("cores.max_latency_ms", c.MaxLatencyMS, 0, 60000),
		inRange("cores.return_after_min", c.ReturnAfterMin, 0, 24*60),
		inRange("updates.interval_hours", s.Updates.IntervalHours, 1, 24*7),
	)

	d := &s.DNS
	d.Remote, d.Direct = strings.TrimSpace(d.Remote), strings.TrimSpace(d.Direct)
	if err := tunlayer.ValidateDNSServer(d.Remote); err != nil {
		errs = append(errs, fmt.Errorf("dns.remote: %w", unprefix(err)))
	}
	if d.Direct != "" {
		if err := tunlayer.ValidateDNSServer(d.Direct); err != nil {
			errs = append(errs, fmt.Errorf("dns.direct: %w", unprefix(err)))
		}
	}

	r := &s.Routing
	r.Mode = strings.ToLower(strings.TrimSpace(r.Mode))
	switch r.Mode {
	case "":
		r.Mode = RouteAll
	case RouteAll, RouteSelected:
	default:
		errs = append(errs, fmt.Errorf("routing.mode: %q is neither %q nor %q", r.Mode, RouteAll, RouteSelected))
	}
	var err error
	r.DirectDomains, err = normalizeList("routing.direct_domains", r.DirectDomains, maxRules, normalizeDomain, strings.EqualFold)
	errs = append(errs, err)
	// The preset used to be these three domains in the list.
	if !r.RussiaDirect && containsAll(r.DirectDomains, russiaSuffixes) {
		r.RussiaDirect = true
		r.DirectDomains = slices.DeleteFunc(r.DirectDomains, func(d string) bool { return slices.Contains(russiaSuffixes, d) })
	}
	r.ProxyDomains, err = normalizeList("routing.proxy_domains", r.ProxyDomains, maxRules, normalizeDomain, strings.EqualFold)
	errs = append(errs, err)
	r.BlockDomains, err = normalizeList("routing.block_domains", r.BlockDomains, maxRules, normalizeDomain, strings.EqualFold)
	errs = append(errs, err)
	r.DirectApps, err = normalizeList("routing.direct_apps", r.DirectApps, maxApps, normalizeApp, strings.EqualFold)
	errs = append(errs, err)
	r.ProxyApps, err = normalizeList("routing.proxy_apps", r.ProxyApps, maxApps, normalizeApp, strings.EqualFold)
	errs = append(errs, err)
	r.AppFilter = strings.ToLower(strings.TrimSpace(r.AppFilter))
	switch r.AppFilter {
	case "":
		r.AppFilter = AppsAll
	case AppsAll, AppsExclude, AppsOnly:
	default:
		errs = append(errs, fmt.Errorf("routing.app_filter: %q is none of %q, %q, %q", r.AppFilter, AppsAll, AppsExclude, AppsOnly))
	}
	r.FilterApps, err = normalizeList("routing.filter_apps", r.FilterApps, maxPackages, normalizePackage, sameString)
	errs = append(errs, err)
	r.DirectIPs, err = normalizeList("routing.direct_ips", r.DirectIPs, maxRules, normalizeIP, sameString)
	errs = append(errs, err)
	r.ProxyIPs, err = normalizeList("routing.proxy_ips", r.ProxyIPs, maxRules, normalizeIP, sameString)
	errs = append(errs, err)
	r.Rules, err = normalizeRules(r.Rules)
	errs = append(errs, err)
	r.Geo, err = normalizeGeo(r.Geo)
	errs = append(errs, err)

	s.Updates.UserAgent = strings.TrimSpace(s.Updates.UserAgent)
	if strings.ContainsAny(s.Updates.UserAgent, "\r\n") {
		errs = append(errs, errors.New("updates.user_agent: must be one line"))
	}
	s.AppUpdate.Source = strings.TrimSpace(s.AppUpdate.Source)
	if s.AppUpdate.Source != "" {
		if _, err := selfupdate.ParseSource(s.AppUpdate.Source); err != nil {
			errs = append(errs, fmt.Errorf("app_update.source: %w", err))
		}
	}
	return s, errors.Join(errs...)
}

var russiaSuffixes = []string{"ru", "su", "xn--p1ai"}

func containsAll(list, want []string) bool {
	for _, w := range want {
		if !slices.Contains(list, w) {
			return false
		}
	}
	return true
}

func inRange(name string, v, lo, hi int) error {
	if v < lo || v > hi {
		return fmt.Errorf("%s: %d is outside %d..%d", name, v, lo, hi)
	}
	return nil
}

// unprefix drops the "tunlayer: " prefix, which means nothing to the user.
func unprefix(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), "tunlayer: "))
}

const (
	maxApps = 200
	// A phone may have many more apps than a PC has programs to list.
	maxPackages = 1000
	maxRules    = 1000
)

// normalizeList tidies every entry with norm, drops empty ones and
// duplicates, and reports the invalid ones under the setting's name.
func normalizeList(name string, raw []string, limit int, norm func(string) (string, error), same func(a, b string) bool) ([]string, error) {
	out := []string{}
	var errs []error
	for _, r := range raw {
		v, err := norm(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		} else if v != "" && !slices.ContainsFunc(out, func(x string) bool { return same(x, v) }) {
			out = append(out, v)
		}
	}
	if len(out) > limit {
		errs = append(errs, fmt.Errorf("%s: at most %d entries", name, limit))
	}
	return out, errors.Join(errs...)
}

func sameString(a, b string) bool { return a == b }

// reservedRanges are the tunnel's own addresses: routing them anywhere but
// through the TUN layer would break every connection by name.
var reservedRanges = []netip.Prefix{
	tunlayer.DefaultFakeIPRange, tunlayer.DefaultFakeIPRange6,
	tunlayer.DefaultAddress.Masked(), tunlayer.DefaultAddress6.Masked(),
}

// normalizeIP turns " 10.8.0.0/16 " or "203.0.113.7" into a masked subnet,
// written as a bare address when it is a single one.
func normalizeIP(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	var p netip.Prefix
	if strings.Contains(v, "/") {
		var err error
		if p, err = netip.ParsePrefix(v); err != nil {
			return "", fmt.Errorf("%q is not an address or subnet", raw)
		}
	} else {
		a, err := netip.ParseAddr(v)
		if err != nil || a.Zone() != "" {
			return "", fmt.Errorf("%q is not an address or subnet", raw)
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	p = p.Masked()
	for _, r := range reservedRanges {
		if p.Overlaps(r) {
			return "", fmt.Errorf("%q overlaps the tunnel's own addresses %s", raw, r)
		}
	}
	if p.IsSingleIP() {
		return p.Addr().String(), nil
	}
	return p.String(), nil
}

// normalizeApp turns "C:\Games\Steam\steam.exe" or " steam " into
// "steam.exe": apps are matched by name, wherever they are installed.
// packageName is an Android application ID: "org.telegram.messenger".
var packageName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

func normalizePackage(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", nil
	}
	if len(p) > 255 || !packageName.MatchString(p) {
		return "", fmt.Errorf("%q is not an Android package name", raw)
	}
	return p, nil
}

func normalizeApp(raw string) (string, error) {
	a := strings.TrimSpace(raw)
	if i := strings.LastIndexAny(a, `\/`); i >= 0 {
		a = a[i+1:]
	}
	a = strings.TrimSpace(a)
	if a == "" {
		return "", nil
	}
	if len(a) > 255 || strings.ContainsAny(a, `<>:"|?*`) || strings.ContainsFunc(a, func(r rune) bool { return r < 0x20 }) {
		return "", fmt.Errorf("%q is not a program name", raw)
	}
	if runtime.GOOS == "windows" && filepath.Ext(a) == "" {
		a += ".exe"
	}
	return a, nil
}

// normalizeDomain turns ".Example.RU." into "example.ru", and a name in
// another script into its punycode, as the cores match it: "Госуслуги.РФ"
// into "xn--c1aapkosapc.xn--p1ai".
func normalizeDomain(raw string) (string, error) {
	d := strings.ToLower(strings.Trim(strings.TrimSpace(raw), "."))
	if d == "" {
		return "", nil
	}
	if strings.ContainsFunc(d, func(r rune) bool { return r > 0x7f }) {
		a, err := idna.Lookup.ToASCII(d)
		if err != nil {
			return "", fmt.Errorf("%q is not a domain", raw)
		}
		d = a
	}
	if len(d) > 253 {
		return "", fmt.Errorf("%q is too long", raw)
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("%q is not a domain", raw)
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return "", fmt.Errorf("%q is not a domain", raw)
			}
		}
	}
	return d, nil
}
