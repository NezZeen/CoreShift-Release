package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"coreshift/engine/internal/service"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/subscription"
)

// runVPN brings the full VPN (TUN + DNS guard + cores) up in the foreground
// until Ctrl+C. It is the quickest way to try the whole engine without the UI.
func runVPN(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vpn", flag.ExitOnError)
	ef := addEngineFlags(fs)
	pick := fs.String("node", "", "use the first node whose name contains this (default: first node)")
	ua := fs.String("ua", subscription.DefaultUserAgent, "User-Agent sent when the file holds a subscription URL")
	verbose := fs.Bool("v", false, "print core output and every health check")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: coreshiftd vpn [flags] <file with a link, a subscription or its URL | ->")
		os.Exit(2)
	}
	if !isElevated() {
		return errors.New("creating the TUN interface and changing system DNS need administrator rights; run from an elevated console")
	}

	n, err := readNode(ctx, fs.Arg(0), *pick, *ua)
	if err != nil {
		return err
	}
	cfg, err := ef.config(true)
	if err != nil {
		return err
	}
	svc, err := service.New(cfg)
	if err != nil {
		return err
	}
	if err := svc.Recover(ctx); err != nil {
		fmt.Println("warning: restoring DNS left by a previous run:", err)
	}
	events, unsubscribe := svc.Subscribe(false)
	defer unsubscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range events {
			printServiceEvent(os.Stdout, e, *verbose)
		}
	}()

	fmt.Printf("node: %s (%s)   chain: %v\n", n.Name, n.Protocol, svc.Compatible(&n))
	if err := svc.Connect(ctx, n); err != nil {
		return err
	}
	fmt.Printf("\nVPN is up: all traffic goes through %q. Ctrl+C to disconnect.\n\n", n.Name)
	<-ctx.Done()
	fmt.Println("\ndisconnecting...")
	svc.Disconnect()
	time.Sleep(200 * time.Millisecond) // let the last events print
	return nil
}

// runServe runs the daemon in the foreground: the service plus its HTTP API.
// Settings and subscriptions come from the store in the data directory.
func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	df := addDaemonFlags(fs)
	apiAddr := fs.String("api", "127.0.0.1:17900", "loopback address of the UI API")
	verbose := fs.Bool("v", false, "print core output and every health check")
	fs.Parse(args)
	cfg, err := df.config()
	if err != nil {
		return err
	}
	return serve(ctx, cfg, *apiAddr, os.Stdout, *verbose)
}

// apiInfo is written to <data dir>/api.json for the UI to find the daemon.
type apiInfo struct {
	Address string `json:"address"`
	Token   string `json:"token"`
}

func serve(ctx context.Context, cfg service.Config, apiAddr string, log io.Writer, verbose bool) error {
	addr, err := parseAddrPort(apiAddr)
	if err != nil || !addr.Addr().IsLoopback() {
		return fmt.Errorf("-api must be a loopback address, got %q", apiAddr)
	}
	if !isElevated() {
		cfg.TUNUnavailable = "TUN mode needs administrator rights, which the daemon does not have; turn TUN off in the settings or run the daemon elevated"
	}
	st, err := service.OpenStore(cfg.DataDir, store.Options{})
	if errors.Is(err, store.ErrReset) {
		fmt.Fprintln(log, "warning:", err)
	} else if err != nil {
		return err
	}
	cfg.Store = st
	svc, err := service.New(cfg)
	if err != nil {
		return err
	}
	if err := svc.Recover(ctx); err != nil {
		fmt.Fprintln(log, "warning: restoring DNS left by a previous run:", err)
	}
	events, unsubscribe := svc.Subscribe(false)
	defer unsubscribe()
	go func() {
		for e := range events {
			printServiceEvent(log, e, verbose)
		}
	}()

	ln, err := net.Listen("tcp", addr.String())
	if err != nil {
		return err
	}
	var raw [32]byte
	rand.Read(raw[:])
	token := hex.EncodeToString(raw[:])
	info, _ := json.MarshalIndent(apiInfo{Address: "http://" + addr.String(), Token: token}, "", "  ")
	infoPath := filepath.Join(cfg.DataDir, "api.json")
	if err := service.WriteShared(infoPath, info); err != nil {
		ln.Close()
		return fmt.Errorf("write %s: %w", infoPath, err)
	}
	defer os.Remove(infoPath)

	srv := &http.Server{Handler: service.NewAPI(svc, token, addr), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	set := st.Settings()
	fmt.Fprintf(log, "API listening on %s (token in %s); %d subscriptions, TUN %v\n",
		addr, infoPath, len(st.Subscriptions()), set.TUN && cfg.TUNUnavailable == "")

	go st.RunUpdater(ctx, time.Minute)
	if set.AutoConnect {
		go autoConnect(ctx, svc, log)
	}

	<-ctx.Done()
	fmt.Fprintln(log, "shutting down")
	svc.Disconnect()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if srv.Shutdown(shutdownCtx) != nil {
		srv.Close() // event streams never go idle
	}
	return nil
}

// autoConnect connects the selected node at start. The network may not be up
// yet when the daemon starts with the system, so failures are retried for a
// while unless the user connects or disconnects in the meantime.
func autoConnect(ctx context.Context, svc *service.Service, log io.Writer) {
	for i, delay := range []time.Duration{0, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute} {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		// After our own failed attempt the state is Failed; anything else
		// means the user took over.
		if st := svc.Status().State; (i == 0 && st != service.Idle) || (i > 0 && st != service.Failed) {
			return
		}
		err := svc.ConnectSelected(ctx)
		if err == nil || errors.Is(err, service.ErrNoSelection) || ctx.Err() != nil {
			if err != nil {
				fmt.Fprintln(log, "auto-connect:", err)
			}
			return
		}
		fmt.Fprintf(log, "auto-connect attempt %d failed: %v\n", i+1, err)
	}
}

func parseAddrPort(s string) (netip.AddrPort, error) { return netip.ParseAddrPort(s) }

func printServiceEvent(w io.Writer, e service.Event, verbose bool) {
	ts := e.Time.Format("15:04:05")
	switch e.Kind {
	case "state":
		line := fmt.Sprintf("%s  STATE      %s", ts, e.State)
		if e.Error != "" {
			line += ": " + e.Error
		}
		fmt.Fprintln(w, line)
	case "core-state":
		fmt.Fprintf(w, "%s  core       %s %s\n", ts, e.Reason, e.Core)
	case "swap":
		fmt.Fprintf(w, "%s  SWAP       %s -> %s  (%s)\n", ts, e.From, e.Core, e.Reason)
	case "core-failed":
		fmt.Fprintf(w, "%s  DROPPED    %s (%s): %s\n", ts, e.Core, e.Reason, e.Error)
	case "tun", "dns":
		if e.Error != "" {
			fmt.Fprintf(w, "%s  %-10s %s\n", ts, e.Kind, e.Error)
		} else {
			fmt.Fprintf(w, "%s  %-10s %s\n", ts, e.Kind, e.Reason)
		}
	case "health":
		if e.Error != "" {
			fmt.Fprintf(w, "%s  health     %s FAILED: %s\n", ts, e.Core, e.Error)
		} else if verbose {
			fmt.Fprintf(w, "%s  health     %s ok %dms\n", ts, e.Core, e.LatencyMS)
		}
	case "store":
		line := fmt.Sprintf("%s  store      %s %s", ts, e.Reason, e.Subscription)
		if e.Error != "" {
			line += ": " + e.Error
		}
		fmt.Fprintln(w, line)
	case "log":
		if verbose || e.Source == "tun" {
			fmt.Fprintf(w, "%s  %-9s| %s\n", ts, e.Source, e.Line)
		}
	}
}
