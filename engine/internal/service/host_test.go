package service

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestServerNameResolvingIntoTunnelRangeIsRefused(t *testing.T) {
	s := &Service{cfg: Config{lookup: func(context.Context, string, netip.AddrPort) (netip.Addr, error) {
		return netip.MustParseAddr("198.18.0.7"), nil
	}}}
	_, err := s.serverAddr(context.Background(), "vpn.example.com")
	if !errors.Is(err, errTunnelRange) || !errors.Is(err, errResolve) {
		t.Fatalf("err = %v", err)
	}
}
