// Command coreshiftd is the privileged CoreShift daemon. Besides the daemon
// itself (serve, service) it exposes its building blocks as subcommands:
//
//	coreshiftd dns resolvers                  print the system resolvers
//	coreshiftd dns apply [-server ip] [-strict]
//	coreshiftd dns revert
//	coreshiftd dns recover                    undo leftovers of a crashed run
//	coreshiftd tun-config [flags] [-o file]   render the TUN + DNS layer config
//	coreshiftd sub [-ua agent] <url|file>     parse a subscription and summarize it
//	coreshiftd sub -shape <file>              the structure of a saved JSON subscription, secrets left out
//	coreshiftd connect [flags] <file|->       serve a node on local SOCKS5 with auto-swap
//	coreshiftd vpn [flags] <file|->           full VPN (TUN + DNS guard) in the foreground
//	coreshiftd serve [flags]                  the daemon with its UI API, in the foreground;
//	                                          settings and subscriptions live in its store
//	coreshiftd service {install|uninstall|start|stop|run}   the Windows service
//	coreshiftd update check [-source github:OWNER/REPO|DIR] [-download DIR]
//	                                          find and verify the latest release of
//	                                          CoreShift, without installing it
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"coreshift/engine/internal/dnsguard"
	"coreshift/engine/internal/service"
	"coreshift/engine/internal/subscription"
	"coreshift/engine/internal/tunlayer"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "dns":
		err = runDNS(ctx, os.Args[2:])
	case "tun-config":
		err = runTunConfig(ctx, os.Args[2:])
	case "sub":
		err = runSub(ctx, os.Args[2:])
	case "connect":
		err = runConnect(ctx, os.Args[2:])
	case "vpn":
		err = runVPN(ctx, os.Args[2:])
	case "serve":
		err = runServe(ctx, os.Args[2:])
	case "service":
		err = runService(ctx, os.Args[2:])
	case "update":
		err = runUpdate(ctx, os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("coreshiftd", service.VersionString())
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: coreshiftd dns {resolvers|apply|revert|recover} [flags]")
	fmt.Fprintln(os.Stderr, "       coreshiftd tun-config [flags]")
	fmt.Fprintln(os.Stderr, "       coreshiftd sub [-ua agent] <url|file>")
	fmt.Fprintln(os.Stderr, "       coreshiftd sub -shape <file>")
	fmt.Fprintln(os.Stderr, "       coreshiftd connect [flags] <file|->")
	fmt.Fprintln(os.Stderr, "       coreshiftd vpn [flags] <file|->")
	fmt.Fprintln(os.Stderr, "       coreshiftd serve [flags]")
	fmt.Fprintln(os.Stderr, "       coreshiftd service {install|uninstall|start|stop|run}")
	fmt.Fprintln(os.Stderr, "       coreshiftd version")
	os.Exit(2)
}

func runDNS(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage()
	}
	cmd := args[0]
	fs := flag.NewFlagSet("dns "+cmd, flag.ExitOnError)
	journal := fs.String("journal", defaultJournalPath(), "change journal used to undo system changes")
	iface := fs.String("iface", tunlayer.DefaultInterface, "TUN interface name")
	server := fs.String("server", tunlayer.DNSAddress(tunlayer.DefaultAddress).String(), "resolver reachable through the TUN")
	strict := fs.Bool("strict", true, "Windows: also disable smart multi-homed name resolution")
	fs.Parse(args[1:])

	if cmd == "resolvers" {
		addrs, err := dnsguard.SystemResolvers(ctx, *iface)
		if err != nil {
			return err
		}
		for _, a := range addrs {
			fmt.Println(a)
		}
		return nil
	}

	g, err := dnsguard.New(*journal)
	if err != nil {
		return err
	}
	switch cmd {
	case "apply":
		addr, err := netip.ParseAddr(*server)
		if err != nil {
			return fmt.Errorf("-server: %w", err)
		}
		return g.Apply(ctx, dnsguard.Config{Interface: *iface, Servers: []netip.Addr{addr}, Strict: *strict})
	case "revert":
		return g.Revert(ctx)
	case "recover":
		return g.Recover(ctx)
	}
	usage()
	return nil
}

func runTunConfig(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tun-config", flag.ExitOnError)
	upstream := fs.String("upstream", "127.0.0.1:17890", "SOCKS5 inbound of the active core")
	remote := fs.String("remote", "https://1.1.1.1/dns-query", "DNS server used through the proxy")
	direct := fs.String("direct", "", "DNS server used without the proxy (default: first system resolver)")
	fakeIP := fs.Bool("fakeip", true, "answer A/AAAA with fake IPs and resolve on the proxy side")
	strictRoute := fs.Bool("strict-route", true, "block traffic trying to bypass the TUN")
	blockDoH := fs.Bool("block-doh", false, "block browsers' built-in DNS over HTTPS")
	blockDoT := fs.Bool("block-dot", false, "block DNS over TLS (port 853)")
	directSuffixes := fs.String("direct-suffix", "", "comma-separated domain suffixes resolved and routed direct")
	bypass := fs.String("bypass-process", "", "comma-separated core executables whose traffic goes direct")
	out := fs.String("o", "", "output file (default stdout)")
	fs.Parse(args)

	up, err := netip.ParseAddrPort(*upstream)
	if err != nil {
		return fmt.Errorf("-upstream: %w", err)
	}
	if *direct == "" {
		addrs, err := dnsguard.SystemResolvers(ctx, tunlayer.DefaultInterface)
		if err != nil {
			return fmt.Errorf("detect system resolver (pass -direct): %w", err)
		}
		if len(addrs) == 0 {
			return errors.New("no system resolver found, pass -direct")
		}
		*direct = addrs[0].String()
	}

	cfg, err := tunlayer.Build(tunlayer.Options{
		StrictRoute:     *strictRoute,
		Upstream:        up,
		BypassProcesses: splitList(*bypass),
		DNS: tunlayer.DNSOptions{
			Remote:          *remote,
			Direct:          *direct,
			FakeIP:          *fakeIP,
			DirectSuffixes:  splitList(*directSuffixes),
			BlockBrowserDoH: *blockDoH,
			BlockDoT:        *blockDoT,
		},
	})
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = os.Stdout.Write(append(cfg, '\n'))
		return err
	}
	return os.WriteFile(*out, cfg, 0o644)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func defaultJournalPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "CoreShift", "dnsguard.json")
	}
	return "/var/lib/coreshift/dnsguard.json"
}

// runSub prints what the engine understood from a subscription. It never
// prints credentials, so the output is safe to share when reporting a bug.
func runSub(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sub", flag.ExitOnError)
	ua := fs.String("ua", subscription.DefaultUserAgent, "User-Agent sent to the panel")
	shape := fs.Bool("shape", false, "print the structure of a JSON subscription saved in a file, with addresses, ids and keys left out")
	fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	src := fs.Arg(0)
	if *shape {
		if isURL(src) {
			return errors.New("-shape reads a file: open the subscription in a browser and save the page, so the link with your token stays out of this")
		}
		body, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		out, err := subscription.Shape(body)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}

	var f subscription.Fetched
	var err error
	if !isURL(src) {
		// A file holding the URL keeps the token out of shell history.
		var body []byte
		if body, err = os.ReadFile(src); err != nil {
			return err
		}
		if u := strings.TrimSpace(string(body)); isURL(u) {
			src = u
		} else {
			f.Result, err = subscription.Parse(body)
		}
	}
	if isURL(src) {
		f, err = subscription.Fetch(ctx, &http.Client{Timeout: 30 * time.Second}, src, *ua)
	}
	if err != nil && len(f.Nodes) == 0 {
		return err
	}

	i := f.Info
	fmt.Printf("format: %s   nodes: %d   skipped: %d\n", f.Format, len(f.Nodes), len(f.Skipped))
	if i.Title != "" {
		fmt.Printf("title: %s\n", i.Title)
	}
	if i.Total > 0 || i.Upload+i.Download > 0 {
		fmt.Printf("traffic: %.2f / %.2f GB\n", float64(i.Upload+i.Download)/1e9, float64(i.Total)/1e9)
	}
	if !i.Expire.IsZero() {
		fmt.Printf("expires: %s\n", i.Expire.Format("2006-01-02"))
	}
	if i.UpdateInterval > 0 {
		fmt.Printf("update every: %s\n", i.UpdateInterval)
	}
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPROTOCOL\tTRANSPORT\tSECURITY\tSERVER\tID")
	for _, n := range f.Nodes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", n.Name, n.Protocol, n.TransportLabel(), n.SecurityLabel(),
			net.JoinHostPort(n.Server, fmt.Sprint(n.Port)), n.Fingerprint())
	}
	w.Flush()
	for _, s := range f.Skipped {
		fmt.Println("skipped", s)
	}
	return nil
}
