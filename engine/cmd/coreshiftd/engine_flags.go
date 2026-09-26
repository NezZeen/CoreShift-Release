package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/service"
	"coreshift/engine/internal/supervisor"
)

// daemonFlags locate the daemon's files and ports; they are the only flags
// of serve and the Windows service, which keep the user's settings in the
// store.
type daemonFlags struct {
	dataDir  *string
	coresDir *string
	listen   *string
}

func addDaemonFlags(fs *flag.FlagSet) *daemonFlags {
	return &daemonFlags{
		dataDir:  fs.String("data-dir", service.DefaultDataDir(), "state directory (settings, subscriptions, DNS journal, API token)"),
		coresDir: fs.String("cores-dir", "", "directory with core executables (default: <exe dir>/cores, else engine/testdata/bin)"),
		listen:   fs.String("listen", core.DefaultListen.String(), "SOCKS5 address of the active core"),
	}
}

func (f *daemonFlags) config() (service.Config, error) {
	dir, err := coresDir(*f.coresDir)
	if err != nil {
		return service.Config{}, err
	}
	bins, err := findCores(dir)
	if err != nil {
		return service.Config{}, err
	}
	listen, err := parseAddrPort(*f.listen)
	if err != nil {
		return service.Config{}, fmt.Errorf("-listen: %w", err)
	}
	return service.Config{DataDir: *f.dataDir, Binaries: bins, Listen: listen}, nil
}

// engineFlags are the settings of the foreground vpn command, which runs
// without the store.
type engineFlags struct {
	daemon      *daemonFlags
	priority    *string
	manual      *string
	remoteDNS   *string
	directDNS   *string
	ruDirect    *bool
	blockDoH    *bool
	blockDoT    *bool
	noFakeIP    *bool
	noIPv6      *bool
	interval    *time.Duration
	returnAfter *time.Duration
}

func addEngineFlags(fs *flag.FlagSet) *engineFlags {
	return &engineFlags{
		daemon:      addDaemonFlags(fs),
		priority:    fs.String("priority", "xray,sing-box,mihomo", "core priority"),
		manual:      fs.String("manual", "", "run only this core, without auto-swap"),
		remoteDNS:   fs.String("remote-dns", service.DefaultDNS.Remote, "DNS server used through the tunnel"),
		directDNS:   fs.String("direct-dns", "", "DNS server for direct names (default: the system's)"),
		ruDirect:    fs.Bool("ru-direct", false, "send Russian sites directly, bypassing the tunnel (.ru, .su, .рф and Russian services and servers elsewhere)"),
		blockDoH:    fs.Bool("block-doh", false, "block browsers' own DNS over HTTPS"),
		blockDoT:    fs.Bool("block-dot", false, "block DNS over TLS (port 853)"),
		noFakeIP:    fs.Bool("no-fakeip", false, "resolve real addresses instead of fake IPs"),
		noIPv6:      fs.Bool("no-ipv6", false, "keep the tunnel IPv4-only"),
		interval:    fs.Duration("health-interval", 15*time.Second, "time between health checks"),
		returnAfter: fs.Duration("return-after", 10*time.Minute, "probe the primary core after this long on a backup (0 = never)"),
	}
}

func (f *engineFlags) config(tun bool) (service.Config, error) {
	cfg, err := f.daemon.config()
	if err != nil {
		return service.Config{}, err
	}
	dns := service.DefaultDNS
	dns.Remote = *f.remoteDNS
	dns.Direct = *f.directDNS
	dns.FakeIP = !*f.noFakeIP
	dns.BlockBrowserDoH = *f.blockDoH
	dns.BlockDoT = *f.blockDoT
	dns.RussiaDirect = *f.ruDirect
	cfg.Options = service.Options{
		Priority:             parseKinds(*f.priority),
		Health:               supervisor.Health{Interval: *f.interval},
		ReturnToPrimaryAfter: *f.returnAfter,
		TUN:                  tun,
		IPv6:                 !*f.noIPv6,
		DNS:                  dns,
	}
	if *f.manual != "" {
		cfg.Mode, cfg.ManualCore = supervisor.Manual, core.Kind(*f.manual)
	}
	return cfg, nil
}

// coresDir picks where core executables live: the flag, a "cores" folder next
// to the executable (installed layout), or the test binaries (development).
func coresDir(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "cores"))
	}
	candidates = append(candidates, filepath.Join("testdata", "bin"), filepath.Join("engine", "testdata", "bin"))
	for _, c := range candidates {
		if dirExists(c) {
			return filepath.Abs(c)
		}
	}
	return "", errors.New("no cores directory found; pass -cores-dir")
}
