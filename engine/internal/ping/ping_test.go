package ping

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestICMPLoopback(t *testing.T) {
	rtt, err := ICMP(context.Background(), netip.MustParseAddr("127.0.0.1"), Bind{}, 2, time.Second)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("ICMP echo needs privileges here")
	}
	if err != nil || rtt <= 0 || rtt > time.Second {
		t.Fatalf("rtt %v, err %v", rtt, err)
	}
}

func TestTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	addr := netip.MustParseAddrPort(ln.Addr().String())
	rtt, err := TCP(context.Background(), addr, Bind{Source: addr.Addr()}, 2, time.Second)
	if err != nil || rtt <= 0 {
		t.Fatalf("rtt %v, err %v", rtt, err)
	}
	ln.Close()
	if _, err := TCP(context.Background(), addr, Bind{}, 1, time.Second); err == nil {
		t.Fatal("closed port answered")
	}
}

func TestPhysical(t *testing.T) {
	b, err := Physical()
	if errors.Is(err, ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Skipf("no default route here: %v", err)
	}
	t.Logf("physical: %v", b)
	if !b.Source.Is4() || b.Interface == "" {
		t.Fatalf("bind = %+v", b)
	}
}
