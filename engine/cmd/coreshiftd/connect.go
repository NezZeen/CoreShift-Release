package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/subscription"
	"coreshift/engine/internal/supervisor"
)

// runConnect serves a node through the supervisor on a local SOCKS5 port,
// without TUN, so auto-swap can be tried against real servers.
//
// The node comes from a file or stdin ("-"), never from the command line,
// where it would end up in shell history and process listings.
func runConnect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	binDir := fs.String("bin-dir", filepath.Join("testdata", "bin"), "directory searched for core executables")
	xrayBin := fs.String("xray", "", "xray executable (overrides -bin-dir)")
	singBoxBin := fs.String("sing-box", "", "sing-box executable (overrides -bin-dir)")
	mihomoBin := fs.String("mihomo", "", "mihomo executable (overrides -bin-dir)")
	priority := fs.String("priority", "xray,sing-box,mihomo", "core priority")
	manual := fs.String("manual", "", "run only this core, without auto-swap")
	pick := fs.String("node", "", "use the first node whose name contains this (default: first node)")
	listen := fs.String("listen", core.DefaultListen.String(), "local SOCKS5 address")
	healthURL := fs.String("health-url", "", "health check URL (default http://cp.cloudflare.com/generate_204)")
	interval := fs.Duration("health-interval", 15*time.Second, "time between health checks")
	returnAfter := fs.Duration("return-after", 10*time.Minute, "probe the primary core after this long on a backup (0 = never)")
	workDir := fs.String("work-dir", filepath.Join(os.TempDir(), "coreshift"), "where generated configs are written")
	verbose := fs.Bool("v", false, "print core output and every health check")
	ua := fs.String("ua", subscription.DefaultUserAgent, "User-Agent sent when the file holds a subscription URL")
	socksUser := fs.String("socks-user", "", "username the SOCKS port requires (default: random)")
	socksPass := fs.String("socks-pass", "", "password the SOCKS port requires (default: random)")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: coreshiftd connect [flags] <file with a link or subscription | ->")
		os.Exit(2)
	}

	n, err := readNode(ctx, fs.Arg(0), *pick, *ua)
	if err != nil {
		return err
	}
	if _, err := os.Stat(*binDir); err != nil && !filepath.IsAbs(*binDir) {
		// Also work when run from the repository root.
		if alt := filepath.Join("engine", *binDir); dirExists(alt) {
			*binDir = alt
		}
	}
	bins, err := findCores(*binDir)
	if err != nil && *xrayBin == "" && *singBoxBin == "" && *mihomoBin == "" {
		return err
	}
	for k, p := range map[core.Kind]string{core.Xray: *xrayBin, core.SingBox: *singBoxBin, core.Mihomo: *mihomoBin} {
		if p != "" {
			bins[k] = p
		}
	}
	addr, err := netip.ParseAddrPort(*listen)
	if err != nil {
		return fmt.Errorf("-listen: %w", err)
	}
	cfg := supervisor.Config{
		Binaries:             bins,
		Priority:             parseKinds(*priority),
		WorkDir:              *workDir,
		Listen:               addr,
		Health:               supervisor.Health{URL: *healthURL, Interval: *interval},
		ReturnToPrimaryAfter: *returnAfter,
		OnEvent:              func(e supervisor.Event) { printEvent(e, *verbose) },
	}
	// The port always requires credentials: any program could use it
	// otherwise. Without the flags they are random, and not printed.
	cfg.Auth = core.NewSOCKSAuth()
	if *socksUser != "" || *socksPass != "" {
		cfg.Auth = core.SOCKSAuth{User: *socksUser, Pass: *socksPass}
	}
	if *manual != "" {
		cfg.Mode, cfg.ManualCore = supervisor.Manual, core.Kind(*manual)
	}
	sup, err := supervisor.New(cfg)
	if err != nil {
		return err
	}

	serverAddr := resolveServer(ctx, n.Server)
	fmt.Printf("node: %s (%s)   chain: %v\n", n.Name, n.Protocol, core.Compatible(&n, installed(cfg.Priority, bins)))
	if err := sup.Connect(ctx, n, serverAddr); err != nil {
		return err
	}
	fmt.Printf("\nSOCKS5 ready on %s - try:  curl.exe -x socks5h://USER:PASS@%s https://ifconfig.me\n"+
		"with -socks-user and -socks-pass as given (without them they are random). Ctrl+C to stop.\n\n", addr, addr)
	<-ctx.Done()
	sup.Disconnect()
	return nil
}

// readNode reads a share link, a subscription body, or a subscription URL
// (which is then downloaded) and picks one node from it.
func readNode(ctx context.Context, src, pick, userAgent string) (node.Node, error) {
	var body []byte
	var err error
	if src == "-" {
		body, err = io.ReadAll(os.Stdin)
	} else {
		body, err = os.ReadFile(src)
	}
	if err != nil {
		return node.Node{}, err
	}
	var res subscription.Result
	if u := strings.TrimSpace(string(body)); isURL(u) {
		fmt.Println("downloading subscription...")
		f, err := subscription.Fetch(ctx, &http.Client{Timeout: 30 * time.Second}, u, userAgent)
		if err != nil {
			return node.Node{}, err
		}
		fmt.Printf("subscription: %d nodes (%s), %d skipped\n", len(f.Nodes), f.Format, len(f.Skipped))
		res = f.Result
	} else if res, err = subscription.Parse(body); err != nil {
		return node.Node{}, err
	}
	for _, n := range res.Nodes {
		if strings.Contains(strings.ToLower(n.Name), strings.ToLower(pick)) {
			return n, nil
		}
	}
	names := make([]string, len(res.Nodes))
	for i, n := range res.Nodes {
		names[i] = fmt.Sprintf("  %2d. %s (%s)", i+1, n.Name, n.Protocol)
	}
	return node.Node{}, fmt.Errorf("no node name contains %q; available:\n%s", pick, strings.Join(names, "\n"))
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// isURL reports whether s is a single http(s) URL, i.e. a subscription link.
func isURL(s string) bool {
	return !strings.ContainsAny(s, "\r\n") && (strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://"))
}

// findCores looks for core executables under dir, as unpacked from releases.
func findCores(dir string) (map[core.Kind]string, error) { return findCoresFor(dir, runtime.GOOS) }

// platformDir matches the folders that hold another platform's cores
// beside these, as engine/testdata/bin does: android-arm64, linux-amd64.
var platformDir = regexp.MustCompile(`^(android|linux|windows|darwin)-[a-z0-9_]+$`)

// findCoresFor is findCores for the executables of goos: .exe files on
// Windows, files without an extension elsewhere.
func findCoresFor(dir, goos string) (map[core.Kind]string, error) {
	bins := map[core.Kind]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && platformDir.MatchString(strings.ToLower(d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		lower := strings.ToLower(d.Name())
		if strings.HasSuffix(lower, ".old") || strings.HasSuffix(lower, ".failed") || strings.HasPrefix(lower, ".") {
			return nil // left over from a core update
		}
		name, exe := strings.CutSuffix(lower, ".exe")
		if exe != (goos == "windows") {
			return nil // another platform's
		}
		switch {
		case name == "xray":
			bins[core.Xray] = path
		case name == "sing-box":
			bins[core.SingBox] = path
		case strings.HasPrefix(name, "mihomo"):
			bins[core.Mihomo] = path
		}
		return nil
	})
	if err == nil && len(bins) == 0 {
		err = errors.New("no core executables found")
	}
	if err != nil {
		return bins, fmt.Errorf("-bin-dir %s: %w", dir, err)
	}
	return bins, nil
}

func parseKinds(s string) []core.Kind {
	var out []core.Kind
	for _, k := range splitList(s) {
		out = append(out, core.Kind(k))
	}
	return out
}

func installed(priority []core.Kind, bins map[core.Kind]string) []core.Kind {
	var out []core.Kind
	for _, k := range priority {
		if bins[k] != "" {
			out = append(out, k)
		}
	}
	return out
}

// resolveServer looks up the server's address with the system resolver, so
// cores never need DNS for their own server (see core.Options.ServerAddr).
func resolveServer(ctx context.Context, host string) string {
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		fmt.Printf("warning: cannot resolve %s (%v); cores will resolve it themselves\n", host, err)
		return ""
	}
	return ips[0].Unmap().String()
}

func printEvent(e supervisor.Event, verbose bool) {
	ts := e.Time.Format("15:04:05")
	probe := ""
	if e.Probe {
		probe = " [probe]"
	}
	switch e.Kind {
	case supervisor.EventState:
		if e.Core != "" {
			fmt.Printf("%s  %-10s %s\n", ts, e.State, e.Core)
		} else {
			fmt.Printf("%s  %s\n", ts, e.State)
		}
	case supervisor.EventSwap:
		fmt.Printf("%s  SWAP       %s -> %s  (%s)\n", ts, e.From, e.Core, e.Reason)
	case supervisor.EventCoreFailed:
		fmt.Printf("%s  DROPPED    %s (%s): %v\n", ts, e.Core, e.Reason, e.Err)
	case supervisor.EventHealth:
		if e.Err != nil {
			fmt.Printf("%s  health     %s%s FAILED: %v\n", ts, e.Core, probe, e.Err)
		} else if verbose {
			fmt.Printf("%s  health     %s%s ok %s\n", ts, e.Core, probe, e.Latency.Round(time.Millisecond))
		}
	case supervisor.EventLog:
		if verbose {
			fmt.Printf("%s  %-9s%s| %s\n", ts, e.Core, probe, e.Line)
		}
	}
}
