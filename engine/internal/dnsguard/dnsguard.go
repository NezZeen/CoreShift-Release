// Package dnsguard makes the operating system send every DNS query into the
// tunnel while it is up, and reliably restores the user's setup afterwards.
//
// Each system change is recorded in an on-disk journal before it is made, so a
// daemon that crashed or was killed can undo it on its next start (see
// Guard.Recover).
//
// Per platform:
//   - Windows: an NRPT rule for "." pointing at the tunnel resolver, plus
//     (Strict) disabling smart multi-homed name resolution. Blocking port 53
//     outside the tunnel is done by the TUN layer's strict_route (WFP).
//   - Linux: systemd-resolved routing domain "~." on the TUN link, or a
//     temporary /etc/resolv.conf when resolved is not in use (kept in place
//     against NetworkManager and DHCP clients, see Keeper).
//   - Android: nothing to do; VpnService owns DNS for routed apps.
package dnsguard

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/netip"
	"strings"
)

// ErrUnsupported is returned on platforms without a guard implementation.
var ErrUnsupported = errors.New("dnsguard: not supported on this platform")

// Config describes the DNS setup enforced while the tunnel is up.
type Config struct {
	// Interface is the TUN interface name. It must already exist on Linux.
	Interface string
	// Servers are resolvers reachable through the tunnel, usually the address
	// right after the TUN's own one (tunlayer.DNSAddress).
	Servers []netip.Addr
	// Strict also disables Windows' smart multi-homed name resolution and
	// parallel A/AAAA queries, which otherwise race the tunnel on physical
	// adapters. Ignored on other platforms.
	Strict bool
}

func (c Config) validate() error {
	if c.Interface == "" {
		return errors.New("dnsguard: interface name is required")
	}
	if len(c.Servers) == 0 {
		return errors.New("dnsguard: at least one DNS server is required")
	}
	for _, s := range c.Servers {
		if !s.IsValid() {
			return errors.New("dnsguard: invalid DNS server address")
		}
	}
	return nil
}

// Guard enforces a Config on the operating system.
type Guard interface {
	// Apply reverts any previous state and then enforces cfg. On failure the
	// system is left as it was before the call.
	Apply(ctx context.Context, cfg Config) error
	// Revert undoes everything Apply changed. It is a no-op when nothing is applied.
	Revert(ctx context.Context) error
	// Recover undoes changes left behind by a previous process that did not
	// shut down cleanly. Call it once on daemon start, before Apply.
	Recover(ctx context.Context) error
}

// Keeper is a Guard whose changes the system may undo behind its back, as
// NetworkManager rewrites /etc/resolv.conf on a DHCP renewal. Keep, called
// now and then while the tunnel is up, puts them back.
type Keeper interface {
	Keep(ctx context.Context) error
}

type noopGuard struct{}

func (noopGuard) Apply(context.Context, Config) error { return nil }
func (noopGuard) Revert(context.Context) error        { return nil }
func (noopGuard) Recover(context.Context) error       { return nil }

func joinAddrs(addrs []netip.Addr, sep string) string {
	s := make([]string, len(addrs))
	for i, a := range addrs {
		s[i] = a.String()
	}
	return strings.Join(s, sep)
}

var siteLocal = netip.MustParsePrefix("fec0::/10")

// usableResolver reports whether a resolver captured from the system can be
// dialled directly: zone-less link-local and the deprecated fec0:: defaults
// Windows reports on IPv6 adapters without real DNS cannot.
func usableResolver(a netip.Addr) bool {
	return a.IsValid() && !a.IsUnspecified() && !a.IsLoopback() && !a.IsMulticast() &&
		!a.IsLinkLocalUnicast() && !siteLocal.Contains(a)
}

// resolvConfNameservers returns every nameserver line of a resolv.conf.
func resolvConfNameservers(b []byte) []netip.Addr {
	var out []netip.Addr
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 || f[0] != "nameserver" {
			continue
		}
		if a, err := netip.ParseAddr(f[1]); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// usesResolvedStub reports whether a resolv.conf points at the local
// systemd-resolved stub, in which case per-link DNS routing takes effect.
func usesResolvedStub(b []byte) bool {
	for _, a := range resolvConfNameservers(b) {
		if s := a.String(); s == "127.0.0.53" || s == "127.0.0.54" {
			return true
		}
	}
	return false
}

// FilterUsable returns the resolvers in addrs that can be dialled directly
// (see usableResolver), each once, in their order. Link-local ones are left
// out with a zone too: a zone names an interface the TUN layer may not
// reach it by (on Android the name cannot even be looked up from Go).
func FilterUsable(addrs []netip.Addr) []netip.Addr {
	var out []netip.Addr
	seen := map[netip.Addr]bool{}
	for _, a := range addrs {
		if usableResolver(a) && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}
