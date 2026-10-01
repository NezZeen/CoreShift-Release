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
	// IPv6 routes IPv6 traffic through the tunnel too, rather than letting
	// it bypass the tunnel.
	IPv6 bool `json:"ipv6"`
	// AutoConnect connects the selected node when the daemon starts.
	AutoConnect bool `json:"auto_connect"`
	// AutoSwitch moves to the next server of the subscription, in its order,
	// when the connected one stops answering and no core can help.
	AutoSwitch bool           `json:"auto_switch"`
	Cores      CoreSettings   `json:"cores"`
	DNS        DNSSettings    `json:"dns"`
	Routing    Routing        `json:"routing"`
	Updates    UpdateSettings `json:"updates"`
	AppUpdate  AppUpdate      `json:"app_update"`
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
)

// Defaults returns the settings of a fresh install.
func Defaults() Settings {
	return Settings{
		TUN:  true,
		IPv6: true,
		Cores: CoreSettings{
			Priority:        []core.Kind{core.Xray, core.SingBox, core.Mihomo},
			Mode:            ModeAuto,
			HealthURL:       "http://cp.cloudflare.com/generate_204",
			HealthIntervalS: 15,
			HealthFailures:  3,
			ReturnAfterMin:  10,
			LatencyTest:     LatencyPing,
		},
		DNS: DNSSettings{
			Remote: "https://1.1.1.1/dns-query",
			FakeIP: true,
			Strict: true,
		},
		Routing: Routing{
			Mode:          RouteAll,
			DirectDomains: []string{}, DirectApps: []string{}, DirectIPs: []string{},
			ProxyDomains: []string{}, ProxyIPs: []string{}, ProxyApps: []string{},
			AppFilter: AppsAll, FilterApps: []string{},
			BlockDomains: []string{},
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

// normalizeDomain turns ".Example.RU." into "example.ru".
func normalizeDomain(raw string) (string, error) {
	d := strings.ToLower(strings.Trim(strings.TrimSpace(raw), "."))
	if d == "" {
		return "", nil
	}
	if len(d) > 253 {
		return "", fmt.Errorf("%q is too long", raw)
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("%q is not a domain", raw)
		}
		for _, r := range label {
			if r > 0x7f {
				return "", fmt.Errorf("%q: write internationalized names in punycode (.рф is xn--p1ai)", raw)
			}
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return "", fmt.Errorf("%q is not a domain", raw)
			}
		}
	}
	return d, nil
}
