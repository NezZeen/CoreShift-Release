package dnsguard

import (
	"context"
	"net/netip"
)

// New returns a guard that changes nothing: on Android the servers passed to
// VpnService.Builder.addDnsServer are used by every app routed through the
// VPN, and the TUN layer's hijack-dns catches queries sent elsewhere.
func New(journalPath string) (Guard, error) { return noopGuard{}, nil }

// SystemResolvers is not available from Go on Android; the app side reads them
// from ConnectivityManager.getLinkProperties and passes them in.
func SystemResolvers(ctx context.Context, exclude string) ([]netip.Addr, error) {
	return nil, ErrUnsupported
}

// SearchDomains is not available from Go on Android either: the app passes
// LinkProperties.getDomains in (mobile.SetNetwork).
func SearchDomains(ctx context.Context, exclude string) ([]string, error) {
	return nil, ErrUnsupported
}
