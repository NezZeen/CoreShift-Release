package dnsprobe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"coreshift/engine/internal/tunlayer"

	"github.com/miekg/dns"
)

func direct(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, addr)
}

func server(t *testing.T, raw string) tunlayer.DNSServer {
	t.Helper()
	s, err := tunlayer.ParseDNSServer(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// answer is a resolver's reply with the code rcode.
func answer(wire []byte, rcode int) []byte {
	q := new(dns.Msg)
	if q.Unpack(wire) != nil {
		return nil
	}
	m := new(dns.Msg)
	m.SetReply(q)
	m.Rcode = rcode
	out, _ := m.Pack()
	return out
}

func udpServer(t *testing.T, rcode int, silent bool) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if !silent {
				pc.WriteTo(answer(buf[:n], rcode), from)
			}
		}
	}()
	return pc.LocalAddr().String()
}

// serveStream answers framed queries on every connection of l.
func serveStream(l net.Listener, rcode int) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			var size [2]byte
			if _, err := io.ReadFull(c, size[:]); err != nil {
				return
			}
			wire := make([]byte, binary.BigEndian.Uint16(size[:]))
			if _, err := io.ReadFull(c, wire); err != nil {
				return
			}
			a := answer(wire, rcode)
			c.Write(binary.BigEndian.AppendUint16(nil, uint16(len(a))))
			c.Write(a)
		}()
	}
}

func closedTCP(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func probe(t *testing.T, raw string, timeout time.Duration) Result {
	t.Helper()
	return Check(context.Background(), server(t, raw), direct, timeout)
}

func TestUDP(t *testing.T) {
	if r := probe(t, "udp://"+udpServer(t, dns.RcodeSuccess, false), time.Second); !r.OK {
		t.Errorf("a resolver that answers: %+v", r)
	}
	// A name that does not exist is an answer too.
	if r := probe(t, "udp://"+udpServer(t, dns.RcodeNameError, false), time.Second); !r.OK {
		t.Errorf("NXDOMAIN: %+v", r)
	}
	// A resolver that says no to us is not one to use.
	if r := probe(t, "udp://"+udpServer(t, dns.RcodeRefused, false), time.Second); r.OK || r.Kind != KindRcode || r.Err != "REFUSED" {
		t.Errorf("REFUSED: %+v", r)
	}
	start := time.Now()
	if r := probe(t, "udp://"+udpServer(t, dns.RcodeSuccess, true), 300*time.Millisecond); r.OK || r.Kind != KindTimeout {
		t.Errorf("silence: %+v", r)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the probe outlasted its timeout: %v", took)
	}
}

func TestTCP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go serveStream(l, dns.RcodeSuccess)
	if r := probe(t, "tcp://"+l.Addr().String(), time.Second); !r.OK {
		t.Errorf("tcp: %+v", r)
	}
	// What the provider's reset of a blocked resolver looks like.
	if r := probe(t, "tcp://"+closedTCP(t), time.Second); r.OK || r.Kind != KindRefused {
		t.Errorf("refused: %+v", r)
	}
	// A connection taken and cut without a word.
	cut, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { cut.Close() })
	go func() {
		for {
			c, err := cut.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if r := probe(t, "tcp://"+cut.Addr().String(), time.Second); r.OK || r.Kind != KindReset {
		t.Errorf("cut: %+v", r)
	}
}

func TestTLS(t *testing.T) {
	cert, pool := selfSigned(t)
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go serveStream(l, dns.RcodeSuccess)

	// Not trusted: what a provider that substitutes its own certificate
	// looks like.
	if r := probe(t, "tls://"+l.Addr().String(), time.Second); r.OK || r.Kind != KindTLS {
		t.Errorf("untrusted certificate: %+v", r)
	}
	rootCAs = pool
	defer func() { rootCAs = nil }()
	if r := probe(t, "tls://"+l.Addr().String(), time.Second); !r.OK {
		t.Errorf("trusted: %+v", r)
	}
}

func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	return ts.TLS.Certificates[0], pool
}

func TestHTTPS(t *testing.T) {
	status := http.StatusOK
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/dns-query" || r.Header.Get("Content-Type") != "application/dns-message" {
			t.Errorf("request = %s %s %v", r.Method, r.URL.Path, r.Header)
		}
		wire, _ := io.ReadAll(r.Body)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(answer(wire, dns.RcodeSuccess))
	}))
	t.Cleanup(ts.Close)
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	rootCAs = pool
	defer func() { rootCAs = nil }()

	raw := "https://" + ts.Listener.Addr().String() + "/dns-query"
	if r := probe(t, raw, 2*time.Second); !r.OK {
		t.Errorf("https: %+v", r)
	}
	status = http.StatusForbidden
	if r := probe(t, raw, 2*time.Second); r.OK || r.Kind != KindHTTP || r.Err != "403" {
		t.Errorf("403: %+v", r)
	}
	// The reported case: a provider that refuses the connection to a
	// resolver by its address.
	if r := probe(t, "https://"+closedTCP(t), time.Second); r.OK || r.Kind != KindRefused {
		t.Errorf("refused: %+v", r)
	}
}

func TestUnsupported(t *testing.T) {
	for _, raw := range []string{"quic://dns.adguard-dns.com", "h3://1.1.1.1/dns-query"} {
		if r := probe(t, raw, time.Second); r.OK || r.Kind != KindUnsupported {
			t.Errorf("%s: %+v", raw, r)
		}
	}
}

// socks5 is a SOCKS5 proxy that wants user and pass (none, if empty) and
// connects to what it is asked for; asked counts the requests.
func socks5(t *testing.T, user, pass string) (addr string, asked *int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	asked = new(int)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				hello := make([]byte, 2)
				if _, err := io.ReadFull(c, hello); err != nil {
					return
				}
				io.ReadFull(c, make([]byte, hello[1]))
				if user == "" {
					c.Write([]byte{5, 0})
				} else {
					c.Write([]byte{5, 2})
					h := make([]byte, 2)
					io.ReadFull(c, h)
					u := make([]byte, h[1])
					io.ReadFull(c, u)
					pl := make([]byte, 1)
					io.ReadFull(c, pl)
					p := make([]byte, pl[0])
					io.ReadFull(c, p)
					if string(u) != user || string(p) != pass {
						c.Write([]byte{1, 1})
						return
					}
					c.Write([]byte{1, 0})
				}
				req := make([]byte, 4)
				if _, err := io.ReadFull(c, req); err != nil {
					return
				}
				var host string
				switch req[3] {
				case 1:
					ip := make([]byte, 4)
					io.ReadFull(c, ip)
					host = net.IP(ip).String()
				case 3:
					n := make([]byte, 1)
					io.ReadFull(c, n)
					h := make([]byte, n[0])
					io.ReadFull(c, h)
					host = string(h)
				}
				p := make([]byte, 2)
				io.ReadFull(c, p)
				*asked++
				up, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(p)))))
				if err != nil {
					c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}()
		}
	}()
	return l.Addr().String(), asked
}

func TestSOCKS5(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go serveStream(l, dns.RcodeSuccess)

	proxyAddr, asked := socks5(t, "user", "secret")
	dial, err := SOCKS5(proxyAddr, "user", "secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if r := Check(context.Background(), server(t, "tcp://"+l.Addr().String()), dial, 2*time.Second); !r.OK || *asked != 1 {
		t.Errorf("through the proxy: %+v, asked %d times", r, *asked)
	}
	// Wrong credentials: the proxy refuses, and that is not the resolver's.
	bad, _ := SOCKS5(proxyAddr, "user", "wrong", time.Second)
	if r := Check(context.Background(), server(t, "tcp://"+l.Addr().String()), bad, 2*time.Second); r.OK {
		t.Errorf("wrong password passed: %+v", r)
	}
	// A resolver the proxy cannot reach is refused through it as well.
	if r := Check(context.Background(), server(t, "tcp://"+closedTCP(t)), dial, 2*time.Second); r.OK || r.Kind != KindRefused && r.Kind != KindOther {
		t.Errorf("unreachable through the proxy: %+v", r)
	}
	// UDP does not go through it.
	if _, err := dial(context.Background(), "udp", "127.0.0.1:53"); err == nil {
		t.Error("UDP dialled through SOCKS5")
	}
}
