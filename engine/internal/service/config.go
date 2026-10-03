package service

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/dnsguard"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/selfupdate"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/supervisor"
)

type DNSSettings struct {
	// Remote resolves proxied names through the tunnel.
	Remote string
	// Direct resolves direct names and bootstraps; empty means the system
	// resolver captured just before connecting.
	Direct         string
	FakeIP         bool
	DirectSuffixes []string
	// RussiaDirect sends Russian sites direct: .ru/.su/.рф plus the geosite
	// and geoip rule sets, downloaded on first use.
	RussiaDirect    bool
	BlockBrowserDoH bool
	BlockDoT        bool
	// Strict also disables Windows' smart multi-homed name resolution.
	Strict bool
}

// Options are what the user can change while the service runs. New values
// take effect on the next Connect.
type Options struct {
	Priority             []core.Kind
	Mode                 supervisor.Mode
	ManualCore           core.Kind
	Health               supervisor.Health
	ReturnToPrimaryAfter time.Duration
	// LatencyTest is store.LatencyPing (the default) or store.LatencyProxy.
	LatencyTest string
	// Fragment splits the TLS ClientHello to get past DPI.
	Fragment bool
	// SwitchServer moves to another server of the subscription when the
	// connected one does not answer (store.CoreSettings.SwitchServer).
	SwitchServer bool

	// TUN routes all system traffic through the tunnel and guards DNS.
	// Without it only the SOCKS port is served.
	TUN bool
	// IPv6 gives the tunnel an IPv6 address too, so IPv6 traffic goes
	// through it instead of around it.
	IPv6 bool
	DNS  DNSSettings
	// DirectApps are executable names whose traffic bypasses the tunnel.
	DirectApps []string
	// DirectIPs bypass the tunnel too; see tunlayer.Options.
	DirectIPs []netip.Prefix
	// The proxy lists always go through the tunnel; with Selective they are
	// the only traffic that does.
	ProxyDomains []string
	ProxyIPs     []netip.Prefix
	ProxyApps    []string
	Selective    bool
	// BlockDomains are refused.
	BlockDomains []string
	// AppFilter and FilterApps choose the Android apps that use the VPN
	// (store.Routing).
	AppFilter  string
	FilterApps []string
}

type Config struct {
	// Options are the initial options; with a Store they come from its
	// settings instead.
	Options

	DataDir  string
	Binaries map[core.Kind]string
	// Listen is the SOCKS port of the active core.
	Listen netip.AddrPort

	// Store, if set, provides settings, subscriptions and the selected node.
	Store *store.Store
	// TUNUnavailable, if set, is why TUN mode cannot work here, e.g. missing
	// privileges; connecting with TUN on then fails with it.
	TUNUnavailable string
	// SelfUpdate lets the service update CoreShift: only the installed
	// service of a release build may run an installer over itself.
	SelfUpdate bool
	// InstallUpdate, if set, hands a verified update to the system's
	// installer, which asks the user (Android): nothing installs by
	// itself, and an update the user declined is offered again.
	InstallUpdate func(path string) error
	// FirstUpdateCheck is how long after the start updates are first
	// looked for; 0 means 2 minutes.
	FirstUpdateCheck time.Duration

	// Hooks for embedding the service in an app (Android); nil means the
	// desktop implementation.
	//
	// TUNLayer runs the TUN + DNS layer.
	TUNLayer TUNLayer
	// SystemResolvers lists the resolvers of the network the device is on.
	SystemResolvers func(ctx context.Context, exclude string) ([]netip.Addr, error)
	// PhysicalBind says how pings of servers leave the device, outside the
	// tunnel.
	PhysicalBind func() (ping.Bind, error)
	// HostIPv6 reports whether the device has IPv6 of its own.
	HostIPv6 func() bool
	// AppOutsideVPN: the platform keeps the app outside its VPN (Android),
	// so the daemon's own lookups of servers go to the system's resolver
	// even while connected; the TUN layer's is out of its reach.
	AppOutsideVPN bool

	// Test seams; nil means the real implementation.
	guard     dnsguard.Guard
	tun       TUNLayer
	resolvers func(ctx context.Context, exclude string) ([]netip.Addr, error)
	// lookup resolves a server name: through server when it is valid,
	// else through the system resolver.
	lookup       func(ctx context.Context, host string, server netip.AddrPort) (netip.Addr, error)
	fetchRuleSet func(ctx context.Context, url string, proxy *url.URL) ([]byte, error)
	hostIPv6     func() bool
	physical     func() (ping.Bind, error)
	icmpPing     func(ctx context.Context, ip netip.Addr, b ping.Bind) (time.Duration, error)
	tcpPing      func(ctx context.Context, ap netip.AddrPort, b ping.Bind) (time.Duration, error)
	netInterval  time.Duration
	speedURL     string // instead of speedServer

	checkRelease     func(ctx context.Context, client *http.Client, src selfupdate.Source) (selfupdate.Release, error)
	downloadRelease  func(ctx context.Context, client *http.Client, rel selfupdate.Release, dir string) (string, error)
	launchInstaller  func(path, logPath string) error
	appSessions      func() []uint32
	startApp         func(sessions []uint32) error
	updateFirstCheck time.Duration
	updateTick       time.Duration
}

// DefaultDataDir is where the daemon keeps its state.
func DefaultDataDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "CoreShift")
	}
	return "/var/lib/coreshift"
}

// DefaultDNS is used when the caller has no preference.
var DefaultDNS = DNSSettings{
	Remote: "https://1.1.1.1/dns-query",
	FakeIP: true,
	Strict: true,
}

// alwaysDirect are local names that never make sense through the tunnel.
var alwaysDirect = []string{"lan", "local", "home.arpa"}

// OptionsFromSettings converts the stored settings.
func OptionsFromSettings(set store.Settings) Options {
	c := set.Cores
	o := Options{
		Priority: slices.Clone(c.Priority),
		Mode:     supervisor.Auto,
		Health: supervisor.Health{
			URL:        c.HealthURL,
			Interval:   time.Duration(c.HealthIntervalS) * time.Second,
			Failures:   c.HealthFailures,
			MaxLatency: time.Duration(c.MaxLatencyMS) * time.Millisecond,
		},
		ReturnToPrimaryAfter: time.Duration(c.ReturnAfterMin) * time.Minute,
		LatencyTest:          c.LatencyTest,
		Fragment:             c.Fragment,
		SwitchServer:         c.SwitchServer,
		TUN:                  set.TUN,
		IPv6:                 set.IPv6,
		DirectApps:           slices.Clone(set.Routing.DirectApps),
		DirectIPs:            prefixes(set.Routing.DirectIPs),
		ProxyDomains:         slices.Clone(set.Routing.ProxyDomains),
		ProxyIPs:             prefixes(set.Routing.ProxyIPs),
		ProxyApps:            slices.Clone(set.Routing.ProxyApps),
		Selective:            set.Routing.Mode == store.RouteSelected,
		BlockDomains:         slices.Clone(set.Routing.BlockDomains),
		AppFilter:            set.Routing.AppFilter,
		FilterApps:           slices.Clone(set.Routing.FilterApps),
		DNS: DNSSettings{
			Remote:          set.DNS.Remote,
			Direct:          set.DNS.Direct,
			FakeIP:          set.DNS.FakeIP,
			DirectSuffixes:  slices.Clone(set.Routing.DirectDomains),
			RussiaDirect:    set.Routing.RussiaDirect,
			BlockBrowserDoH: set.DNS.BlockBrowserDoH,
			BlockDoT:        set.DNS.BlockDoT,
			Strict:          set.DNS.Strict,
		},
	}
	if c.Mode == store.ModeManual {
		o.Mode, o.ManualCore = supervisor.Manual, c.Manual
	}
	return o
}

// prefixes parses the store's address lists, which it has validated.
func prefixes(list []string) []netip.Prefix {
	var out []netip.Prefix
	for _, v := range list {
		if p, err := netip.ParsePrefix(v); err == nil {
			out = append(out, p)
		} else if a, err := netip.ParseAddr(v); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out
}

func (o Options) withDefaults() Options {
	if o.DNS.Remote == "" {
		// No DNS preferences: the defaults, keeping what the caller did set.
		d := DefaultDNS
		d.Direct, d.DirectSuffixes, d.RussiaDirect = o.DNS.Direct, o.DNS.DirectSuffixes, o.DNS.RussiaDirect
		d.BlockBrowserDoH, d.BlockDoT = o.DNS.BlockBrowserDoH, o.DNS.BlockDoT
		o.DNS = d
	}
	if len(o.Priority) == 0 {
		for _, a := range core.Adapters() {
			o.Priority = append(o.Priority, a.Kind())
		}
	}
	return o
}

func (o Options) policy() supervisor.Policy {
	return supervisor.Policy{
		Priority: o.Priority, Mode: o.Mode, ManualCore: o.ManualCore,
		Health: o.Health, ReturnToPrimaryAfter: o.ReturnToPrimaryAfter, Fragment: o.Fragment,
	}
}
