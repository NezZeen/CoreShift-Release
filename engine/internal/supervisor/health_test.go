package supervisor

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

// slowSOCKS is a SOCKS5 proxy that takes setup to set each connection up,
// as a proxy's handshake with a distant server does, and counts them.
func slowSOCKS(t *testing.T, setup time.Duration) (netip.AddrPort, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var conns atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go serveSOCKS(c, setup)
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String()), &conns
}

func serveSOCKS(c net.Conn, setup time.Duration) {
	defer c.Close()
	buf := make([]byte, 262)
	// Greeting: version, methods; no authentication.
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return
	}
	if _, err := io.ReadFull(c, buf[:buf[1]]); err != nil {
		return
	}
	c.Write([]byte{5, 0})
	// CONNECT request.
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	var host string
	switch buf[3] {
	case 1:
		io.ReadFull(c, buf[:4])
		host = net.IP(buf[:4]).String()
	case 3:
		io.ReadFull(c, buf[:1])
		n := int(buf[0])
		io.ReadFull(c, buf[:n])
		host = string(buf[:n])
	default:
		return
	}
	io.ReadFull(c, buf[:2])
	port := binary.BigEndian.Uint16(buf[:2])
	time.Sleep(setup)
	up, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	go io.Copy(up, c)
	io.Copy(c, up)
}

func TestDelayLeavesOutConnectionSetup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	socks, conns := slowSOCKS(t, 300*time.Millisecond)

	lat, err := delayThrough(context.Background(), core.SOCKSAuth{}.ProxyURL(socks), srv.URL+"/generate_204", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// The second request reuses the connection the first one set up.
	if lat >= 200*time.Millisecond {
		t.Errorf("delay %v includes the connection's setup", lat)
	}
	if n := conns.Load(); n != 1 {
		t.Errorf("%d connections through the proxy, want 1", n)
	}

	// A fresh connection per request, as the health check makes, pays the
	// setup each time.
	full, err := fetchThrough(context.Background(), core.SOCKSAuth{}.ProxyURL(socks), srv.URL+"/generate_204")
	if err != nil || full < 300*time.Millisecond {
		t.Errorf("health check: %v, %v", full, err)
	}
}
