//go:build !linux

package service

import (
	"context"
	"net"
	"net/netip"
)

// systemLookup resolves host as other programs do. On Windows that is the
// DNS Client service's job, so the query reaches the tunnel from that
// service rather than from the daemon, whose own lookups the TUN layer
// answers with the direct resolver (tunlayer.Options.DirectDNSProcesses).
func systemLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
