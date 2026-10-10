package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/sysproxy"
)

// Guard's pace: the port is looked at every guardEvery, and the proxy
// restored once it has not answered for guardGrace (a reconnect closes it
// for a moment).
const (
	guardEvery = 2 * time.Second
	guardGrace = 15 * time.Second
)

// runSysProxy is `coreshiftd sysproxy`, which the app runs as the user
// (never the daemon: the proxy is the user's setting, see the sysproxy
// package). It prints a sysproxy.Report as one line of JSON.
//
//	apply [-addr 127.0.0.1:17890]   set the system proxy to the port, then
//	                                watch it in the background (guard)
//	restore                         put back what was there before
//	logon                           at sign-in: restore unless the port answers
//	guard                           restore once the port has been dead a while
//	status                          whether there is anything to put back
func runSysProxy(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage()
	}
	cmd := args[0]
	fs := flag.NewFlagSet("sysproxy "+cmd, flag.ExitOnError)
	addrFlag := fs.String("addr", core.DefaultListen.String(), "the local proxy port the system proxy points at")
	fs.Parse(args[1:])

	m, err := sysproxy.Default(ctx)
	if err != nil {
		printReport(sysproxy.Report{Action: cmd, Backends: []string{}, Errors: []string{err.Error()}})
		return err
	}
	var r sysproxy.Report
	switch cmd {
	case "apply":
		addr, err := netip.ParseAddrPort(*addrFlag)
		// Only CoreShift's own port: a proxy elsewhere is not ours to set.
		if err != nil || !addr.Addr().IsLoopback() {
			return fmt.Errorf("-addr must be a loopback address and port, got %q", *addrFlag)
		}
		r = m.Apply(ctx, addr)
		if r.Pending {
			if err := startGuard(); err != nil {
				r.Errors = append(r.Errors, "guard: "+err.Error())
			}
		}
	case "restore":
		r = m.Restore(ctx)
	case "logon":
		r = m.Logon(ctx)
	case "guard":
		r = m.GuardOnce(ctx, guardEvery, guardGrace)
	case "status":
		r = m.Status()
	default:
		usage()
	}
	printReport(r)
	return nil
}

func printReport(r sysproxy.Report) {
	b, _ := json.Marshal(r)
	fmt.Println(string(b))
}

// startGuard starts `coreshiftd sysproxy guard` on its own, outliving this
// process and the app that started it (detach).
func startGuard() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	for _, breakaway := range []bool{true, false} {
		cmd := exec.Command(self, "sysproxy", "guard")
		detach(cmd, breakaway)
		if err = cmd.Start(); err == nil {
			return cmd.Process.Release()
		}
	}
	return err
}
