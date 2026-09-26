//go:build !windows && !linux

package dnsguard

import (
	"context"
	"net/netip"
)

func New(journalPath string) (Guard, error) { return nil, ErrUnsupported }

func SystemResolvers(ctx context.Context, exclude string) ([]netip.Addr, error) {
	return nil, ErrUnsupported
}
