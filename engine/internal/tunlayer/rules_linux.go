//go:build linux && !android

package tunlayer

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strconv"
)

// CleanupRoutes removes the ip rules and routes a TUN layer that did not
// shut down (kill -9, a crash) left behind. Call it at start, before a TUN
// layer runs: while the interface iface exists it does nothing.
func CleanupRoutes(ctx context.Context, iface string) error {
	if _, err := net.InterfaceByName(iface); err == nil {
		return nil
	}
	if _, err := exec.LookPath("ip"); err != nil {
		return nil // nothing to clean with; the rules point at an empty table
	}
	var errs []error
	for _, family := range []string{"-4", "-6"} {
		out, err := exec.CommandContext(ctx, "ip", family, "rule", "show").Output()
		if err != nil {
			continue // no IPv6 here, say
		}
		for _, p := range staleRulePrefs(out) {
			if err := exec.CommandContext(ctx, "ip", family, "rule", "del", "pref", strconv.Itoa(p)).Run(); err != nil {
				errs = append(errs, err)
			}
		}
		_ = exec.CommandContext(ctx, "ip", family, "route", "flush", "table", strconv.Itoa(RouteTable)).Run()
	}
	if len(errs) > 0 {
		return errors.Join(append([]error{errors.New("tunlayer: remove stale ip rules")}, errs...)...)
	}
	return nil
}
