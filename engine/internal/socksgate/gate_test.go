package socksgate

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/protocol/socks"

	"coreshift/engine/internal/core"
)

// fakeCore is a SOCKS5 server as the cores run one: it requires its
// credentials, connects CONNECT requests directly, and answers UDP
// ASSOCIATE with a UDP relay of its own, on a port of its own.
type fakeCore struct {
	ln    net.Listener
	auth  core.SOCKSAuth
	conns atomic.Int64 // connections that got past authentication
	reqs  atomic.Int64 // requests that came after it
	mu    sync.Mutex
	open  []net.Conn
}

func startFakeCore(t testing.TB, auth core.SOCKSAuth) *fakeCore {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeCore{ln: ln, auth: auth}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.open = append(f.open, c)
			f.mu.Unlock()
			go f.serve(c)
		}
	}()
	t.Cleanup(f.stop)
	return f
}

func (f *fakeCore) addr() netip.AddrPort { return netip.MustParseAddrPort(f.ln.Addr().String()) }

func (f *fakeCore) target() Target { return Target{Addr: f.addr(), Auth: f.auth} }

// stop closes the port and every connection, as a core killed.
func (f *fakeCore) stop() {
	f.ln.Close()
	f.mu.Lock()
	for _, c := range f.open {
		c.Close()
	}
	f.mu.Unlock()
}

func (f *fakeCore) serve(c net.Conn) {
	defer c.Close()
	var hdr [2]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil || hdr[0] != 5 {
		return
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	if !bytes.Contains(methods, []byte{2}) {
		c.Write([]byte{5, 0xff})
		return
	}
	c.Write([]byte{5, 2})
	user, pass, err := readUserPass(c)
	if err != nil || string(user) != f.auth.User || string(pass) != f.auth.Pass {
		c.Write([]byte{1, 1})
		return
	}
	c.Write([]byte{1, 0})
	f.conns.Add(1)
	var req [4]byte
	if _, err := io.ReadFull(c, req[:]); err != nil {
		return
	}
	dst, err := readAddr(c, req[3])
	if err != nil {
		return
	}
	f.reqs.Add(1)
	switch req[1] {
	case 1: // CONNECT
		up, err := net.Dial("tcp", dst)
		if err != nil {
			c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		defer up.Close()
		la := up.LocalAddr().(*net.TCPAddr)
		reply := append([]byte{5, 0, 0, 1}, la.IP.To4()...)
		c.Write(binary.BigEndian.AppendUint16(reply, uint16(la.Port)))
		relay(c, up)
	case 3: // UDP ASSOCIATE
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			return
		}
		defer pc.Close()
		la := pc.LocalAddr().(*net.UDPAddr)
		reply := append([]byte{5, 0, 0, 1}, la.IP.To4()...)
		c.Write(binary.BigEndian.AppendUint16(reply, uint16(la.Port)))
		go udpRelay(pc)
		io.Copy(io.Discard, c) // the association lasts while this does
	default:
		c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
	}
}

func readAddr(r io.Reader, atyp byte) (string, error) {
	var host string
	switch atyp {
	case 1, 4:
		b := make([]byte, map[byte]int{1: 4, 4: 16}[atyp])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 3:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return "", err
		}
		b := make([]byte, l[0])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", errors.New("bad address type")
	}
	var p [2]byte
	if _, err := io.ReadFull(r, p[:]); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(p[:]))), nil
}

// udpRelay sends each datagram to its destination and wraps the answers,
// for one client: the first that writes.
func udpRelay(pc net.PacketConn) {
	var client atomic.Pointer[net.Addr]
	out, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return
	}
	defer out.Close()
	go func() {
		buf := make([]byte, 65536)
		for {
			n, from, err := out.ReadFrom(buf)
			if err != nil {
				return
			}
			ua := from.(*net.UDPAddr)
			hdr := append([]byte{0, 0, 0, 1}, ua.IP.To4()...)
			hdr = binary.BigEndian.AppendUint16(hdr, uint16(ua.Port))
			if c := client.Load(); c != nil {
				pc.WriteTo(append(hdr, buf[:n]...), *c)
			}
		}
	}()
	buf := make([]byte, 65536)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		client.Store(&from)
		r := bytes.NewReader(buf[3:n])
		var atyp [1]byte
		r.Read(atyp[:])
		dst, err := readAddr(r, atyp[0])
		if err != nil {
			continue
		}
		ua, err := net.ResolveUDPAddr("udp", dst)
		if err != nil {
			continue
		}
		out.WriteTo(buf[n-r.Len():n], ua)
	}
}

func newGate(t testing.TB, cfg Config) *Gate {
	t.Helper()
	if !cfg.Listen.IsValid() {
		cfg.Listen = netip.MustParseAddrPort("127.0.0.1:0")
	}
	g, err := Listen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

func webServer(t testing.TB) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") }))
	t.Cleanup(srv.Close)
	return srv
}

func get(proxy *url.URL, target string) error {
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	resp, err := c.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err == nil && string(b) != "hello" {
		err = fmt.Errorf("got %q", b)
	}
	return err
}

var (
	clientAuth = core.SOCKSAuth{User: "tun", Pass: "client-secret"}
	coreAuth   = core.SOCKSAuth{User: "core", Pass: "core-secret"}
)

// With credentials, as for the TUN layer and on Android, a client without
// them or with wrong ones gets nowhere, and the core's credentials are
// the engine's alone.
func TestCredentialsRequired(t *testing.T) {
	web := webServer(t)
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth})
	g.SetTarget(f.target())
	for name, a := range map[string]core.SOCKSAuth{
		"none":       {},
		"wrong":      {User: clientAuth.User, Pass: "wrong"},
		"the core's": coreAuth,
	} {
		if err := get(a.ProxyURL(g.Addr()), web.URL); err == nil {
			t.Errorf("%s credentials: let through", name)
		}
	}
	if err := get(clientAuth.ProxyURL(g.Addr()), web.URL); err != nil {
		t.Fatalf("with the credentials: %v", err)
	}
	// The gate connects to the core while a client greets, but only an
	// authenticated client's request gets there.
	if n := f.reqs.Load(); n != 1 {
		t.Errorf("the core got %d requests, want only the authenticated client's", n)
	}
}

// Without the TUN layer the gate is the proxy of the user's programs:
// open, while the core behind it still requires its credentials.
func TestOpenGate(t *testing.T) {
	web := webServer(t)
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth, Open: true})
	g.SetTarget(f.target())
	if err := get(core.SOCKSAuth{}.ProxyURL(g.Addr()), web.URL); err != nil {
		t.Fatalf("without credentials: %v", err)
	}
	// A program set up with some user and password of its own.
	if err := get(core.SOCKSAuth{User: "x", Pass: "y"}.ProxyURL(g.Addr()), web.URL); err != nil {
		t.Fatalf("with other credentials: %v", err)
	}
	if err := get(core.SOCKSAuth{}.ProxyURL(f.addr()), web.URL); err == nil {
		t.Error("the core itself let in a client without credentials")
	}
}

// UDP goes as the TUN layer sends it: sing's SOCKS client associates
// through the gate and talks to the relay the core named.
func TestUDPAssociate(t *testing.T) {
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			echo.WriteTo(append([]byte("echo:"), buf[:n]...), from)
		}
	}()
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth})
	g.SetTarget(f.target())

	dst := metadata.ParseSocksaddr(echo.LocalAddr().String())
	client := socks.NewClient(N.SystemDialer, metadata.ParseSocksaddr(g.Addr().String()), socks.Version5, clientAuth.User, clientAuth.Pass)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pc, err := client.ListenPacket(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(5 * time.Second))
	for i := range 3 {
		msg := fmt.Sprintf("ping %d", i)
		if _, err := pc.WriteTo([]byte(msg), dst.UDPAddr()); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 2048)
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(buf[:n]); got != "echo:"+msg {
			t.Fatalf("got %q", got)
		}
	}

	// Without the gate's credentials there is no association.
	bad := socks.NewClient(N.SystemDialer, metadata.ParseSocksaddr(g.Addr().String()), socks.Version5, "", "")
	if pc, err := bad.ListenPacket(ctx, dst); err == nil {
		pc.Close()
		t.Error("associated without credentials")
	}
}

// A swap: the connections to the old core are closed, new ones reach the
// new core, and one made while no core runs waits for the next.
func TestSwap(t *testing.T) {
	web := webServer(t)
	a, b := startFakeCore(t, coreAuth), startFakeCore(t, core.SOCKSAuth{User: "core2", Pass: "other"})
	g := newGate(t, Config{Auth: clientAuth})
	g.SetTarget(a.target())

	// A connection held open to the first core.
	held, err := net.Dial("tcp", g.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	held.Write(append([]byte{5, 1, 2, 1, byte(len(clientAuth.User))}, append([]byte(clientAuth.User), append([]byte{byte(len(clientAuth.Pass))}, clientAuth.Pass...)...)...))
	reply := make([]byte, 4)
	if _, err := io.ReadFull(held, reply); err != nil || !bytes.Equal(reply, []byte{5, 2, 1, 0}) {
		t.Fatalf("handshake %v %x", err, reply)
	}
	waitFor(t, "connected to a", func() bool { return a.conns.Load() == 1 })

	// The core stops: until the next is up, a client waits.
	g.SetTarget(Target{})
	a.stop()
	held.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := held.Read(make([]byte, 1)); err == nil {
		t.Error("the connection to the stopped core stayed open")
	}
	errc := make(chan error, 1)
	go func() { errc <- get(clientAuth.ProxyURL(g.Addr()), web.URL) }()
	time.Sleep(300 * time.Millisecond)
	g.SetTarget(b.target())
	if err := <-errc; err != nil {
		t.Fatalf("a connection made during the swap: %v", err)
	}
	if err := get(clientAuth.ProxyURL(g.Addr()), web.URL); err != nil {
		t.Fatalf("after the swap: %v", err)
	}
	if b.reqs.Load() != 2 {
		t.Errorf("the new core got %d requests", b.reqs.Load())
	}
}

// With no core for longer than Wait the client is told so.
func TestNoCore(t *testing.T) {
	g := newGate(t, Config{Auth: clientAuth, Wait: 200 * time.Millisecond})
	err := get(clientAuth.ProxyURL(g.Addr()), "http://example.invalid/")
	if err == nil || !strings.Contains(err.Error(), "general") && !strings.Contains(err.Error(), "failure") {
		t.Fatalf("err = %v", err)
	}
}

// The port stays the gate's through any number of swaps, and closing frees
// it and every connection.
func TestPortHeldThroughSwaps(t *testing.T) {
	g := newGate(t, Config{Auth: clientAuth})
	for i := range 20 {
		f := startFakeCore(t, coreAuth)
		g.SetTarget(f.target())
		g.SetTarget(Target{})
		f.stop()
		if ln, err := net.Listen("tcp", g.Addr().String()); err == nil {
			ln.Close()
			t.Fatalf("swap %d: another listener took the port", i)
		}
	}
	// Nor does one asking to share the port: on Windows SO_REUSEADDR
	// would let it in beside a socket that does not forbid it.
	lc := net.ListenConfig{Control: reuse}
	if ln, err := lc.Listen(context.Background(), "tcp", g.Addr().String()); err == nil {
		ln.Close()
		t.Fatal("a listener asking to share the port took it")
	}
	addr := g.Addr()
	idle, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	waitFor(t, "a session", func() bool { return g.Sessions() == 1 })
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if g.Sessions() != 0 {
		t.Error("sessions left after Close")
	}
	ln, err := net.Listen("tcp", addr.String())
	if err != nil {
		t.Fatalf("the port is not free after Close: %v", err)
	}
	ln.Close()
}

func waitFor(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: never happened", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
