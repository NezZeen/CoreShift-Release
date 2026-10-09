package socksgate

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

// The gate counts what passes it: a download's every byte down, its
// request up, while the download lasts and not only once it ends.
func TestTCPTrafficCounted(t *testing.T) {
	const size = 4 << 20
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), size/2))
		w.(http.Flusher).Flush()
		<-release
		w.Write(bytes.Repeat([]byte("x"), size/2))
	}))
	defer srv.Close()
	defer close(release)
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth})
	g.SetTarget(f.target())

	c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(clientAuth.ProxyURL(g.Addr()))}}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	waitFor(t, "half the download counted", func() bool { return g.Traffic().Down >= size/2 })
	if tr := g.Traffic(); tr.Down >= size || tr.Up < int64(len("GET / HTTP/1.1\r\n")) {
		t.Errorf("mid-download traffic %+v", tr)
	}
	release <- struct{}{}
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil || n != size {
		t.Fatalf("read %d: %v", n, err)
	}
	if tr := g.Traffic(); tr.Down < size || tr.Down > size+4096 {
		t.Errorf("traffic %+v after %d bytes", tr, size)
	}
}

// associate does a UDP ASSOCIATE at gate the way a client does, and
// returns the relay the gate names and the connection that keeps it.
func associate(t *testing.T, gate netip.AddrPort, auth core.SOCKSAuth) (netip.AddrPort, net.Conn) {
	t.Helper()
	c, err := net.Dial("tcp", gate.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(5 * time.Second))
	msg := append([]byte{5, 1, 2, 1, byte(len(auth.User))}, auth.User...)
	msg = append(append(msg, byte(len(auth.Pass))), auth.Pass...)
	msg = append(msg, 5, cmdUDPAssociate, 0, atypIPv4, 0, 0, 0, 0, 0, 0)
	if _, err := c.Write(msg); err != nil {
		t.Fatal(err)
	}
	ack := make([]byte, 4)
	if _, err := io.ReadFull(c, ack); err != nil || !bytes.Equal(ack, []byte{5, 2, 1, 0}) {
		t.Fatalf("handshake % x: %v", ack, err)
	}
	rep, err := readReply(c)
	if err != nil || rep.code != 0 {
		t.Fatalf("associate: %+v %v", rep, err)
	}
	c.SetDeadline(time.Time{})
	return rep.addr, c
}

// datagram wraps msg for dst in the SOCKS5 UDP header.
func datagram(dst netip.AddrPort, msg string) []byte {
	return append(appendAddr([]byte{0, 0, 0}, dst), msg...)
}

// UDP passes the gate too: the relay it names is its own, on loopback,
// in front of the core's; the datagrams' data is counted without the
// headers; and only the client that first used the relay is served.
func TestUDPThroughTheGate(t *testing.T) {
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	var echoed atomic.Int32
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			echoed.Add(1)
			echo.WriteTo(append([]byte("echo:"), buf[:n]...), from)
		}
	}()
	dst := netip.MustParseAddrPort(echo.LocalAddr().String())
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth})
	g.SetTarget(f.target())

	relayAt, ctl := associate(t, g.Addr(), clientAuth)
	if !relayAt.Addr().IsLoopback() || relayAt.Port() == f.addr().Port() {
		t.Fatalf("relay at %v", relayAt)
	}
	pc, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 2048)
	var up, down int
	for i := range 3 {
		msg := strings.Repeat("p", 100*(i+1))
		if _, err := pc.WriteToUDPAddrPort(datagram(dst, msg), relayAt); err != nil {
			t.Fatal(err)
		}
		n, _, err := pc.ReadFromUDPAddrPort(buf)
		if err != nil {
			t.Fatal(err)
		}
		got := buf[udpHeaderLen(buf[:n]):n]
		if string(got) != "echo:"+msg {
			t.Fatalf("got %q", got)
		}
		up, down = up+len(msg), down+len(got)
	}
	if tr := g.Traffic(); tr.Up != int64(up) || tr.Down != int64(down) {
		t.Errorf("traffic %+v, want up %d down %d", tr, up, down)
	}

	// Another program that finds the relay gets nothing through it.
	other, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	before := echoed.Load()
	other.WriteToUDPAddrPort(datagram(dst, "intruder"), relayAt)
	other.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if n, _, err := other.ReadFromUDPAddrPort(buf); err == nil {
		t.Errorf("the other program got %q", buf[:n])
	}
	if echoed.Load() != before {
		t.Error("the other program's datagram went through")
	}

	// The association ends with its connection.
	ctl.Close()
	waitFor(t, "the relay closed", func() bool { return g.Sessions() == 0 })
	pc.WriteToUDPAddrPort(datagram(dst, "late"), relayAt)
	pc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := pc.ReadFromUDPAddrPort(buf); err == nil {
		t.Error("the relay still answers after its connection closed")
	}
}

func TestUDPHeaderLen(t *testing.T) {
	v4 := datagram(netip.MustParseAddrPort("192.0.2.1:53"), "q")
	v6 := datagram(netip.MustParseAddrPort("[2001:db8::1]:53"), "q")
	name := append([]byte{0, 0, 0, atypDomain, 11}, "example.com"...)
	name = binary.BigEndian.AppendUint16(name, 443)
	for _, c := range []struct {
		b    []byte
		want int
	}{
		{v4, 10}, {v6, 22}, {append(name, 'q'), 18}, {name[:8], 0}, {[]byte{0, 0}, 0}, {[]byte{0, 0, 0, 9, 1}, 0},
	} {
		if got := udpHeaderLen(c.b); got != c.want {
			t.Errorf("% x: %d, want %d", c.b, got, c.want)
		}
	}
}
