package doh

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// fakeDoH is a DNS-over-HTTPS server on loopback. Its certificate is for
// example.com, so the resolver must dial the fixed address and still check
// the name.
type fakeDoH struct {
	srv     *httptest.Server
	queries atomic.Int32
}

func newFakeDoH(t *testing.T, answer func(w http.ResponseWriter, r *http.Request, q *dns.Msg)) *fakeDoH {
	t.Helper()
	f := &fakeDoH{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.queries.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/dns-query" || r.Header.Get("Content-Type") != "application/dns-message" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if r.Host != "example.com" {
			http.Error(w, "wrong host "+r.Host, http.StatusMisdirectedRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		q := new(dns.Msg)
		if err := q.Unpack(body); err != nil || len(q.Question) != 1 {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		answer(w, r, q)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDoH) server() Server {
	return Server{Addr: netip.MustParseAddrPort(f.srv.Listener.Addr().String()), URL: "https://example.com/dns-query"}
}

func (f *fakeDoH) roots() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(f.srv.Certificate())
	return p
}

// reply answers q with rcode and records "name TTL IN TYPE value", keeping
// only those of the type asked for, as a resolver does.
func reply(w http.ResponseWriter, q *dns.Msg, rcode int, records ...string) {
	a := new(dns.Msg)
	a.SetRcode(q, rcode)
	for _, s := range records {
		rr, err := dns.NewRR(s)
		if err != nil {
			panic(err)
		}
		if rr.Header().Rrtype == q.Question[0].Qtype {
			a.Answer = append(a.Answer, rr)
		}
	}
	b, _ := a.Pack()
	w.Header().Set("Content-Type", "application/dns-message")
	w.Write(b)
}

func newResolver(timeout time.Duration, roots *x509.CertPool, servers ...Server) *Resolver {
	return &Resolver{Servers: servers, RootCAs: roots, Timeout: timeout}
}

func TestLookupA(t *testing.T) {
	f := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, q *dns.Msg) {
		if q.Question[0].Name != "vpn.example.net." {
			reply(w, q, dns.RcodeNameError)
			return
		}
		reply(w, q, dns.RcodeSuccess, "vpn.example.net. 60 IN A 192.0.2.10", "vpn.example.net. 60 IN AAAA 2001:db8::10")
	})
	r := newResolver(time.Second, f.roots(), f.server())
	defer r.CloseIdleConnections()
	addrs, err := r.Lookup(context.Background(), "vpn.example.net")
	if err != nil || len(addrs) != 1 || addrs[0] != netip.MustParseAddr("192.0.2.10") {
		t.Fatalf("addrs %v, err %v", addrs, err)
	}
	// An address needs no server.
	before := f.queries.Load()
	if addrs, err := r.Lookup(context.Background(), "203.0.113.4"); err != nil || addrs[0] != netip.MustParseAddr("203.0.113.4") {
		t.Errorf("address: %v %v", addrs, err)
	}
	if f.queries.Load() != before {
		t.Error("an address was asked about")
	}
}

func TestLookupAAAAOnly(t *testing.T) {
	f := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, q *dns.Msg) {
		reply(w, q, dns.RcodeSuccess, "v6.example.net. 60 IN AAAA 2001:db8::7")
	})
	r := newResolver(time.Second, f.roots(), f.server())
	addrs, err := r.Lookup(context.Background(), "v6.example.net")
	if err != nil || len(addrs) != 1 || addrs[0] != netip.MustParseAddr("2001:db8::7") {
		t.Fatalf("addrs %v, err %v", addrs, err)
	}
}

func TestLookupNXDOMAIN(t *testing.T) {
	f := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, q *dns.Msg) {
		reply(w, q, dns.RcodeNameError)
	})
	r := newResolver(time.Second, f.roots(), f.server())
	_, err := r.Lookup(context.Background(), "gone.example.net")
	var de *net.DNSError
	if !errors.As(err, &de) || !de.IsNotFound || de.IsTimeout {
		t.Fatalf("err %v (%#v)", err, de)
	}
}

// A name with no addresses at all is not found either.
func TestLookupNoRecords(t *testing.T) {
	f := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, q *dns.Msg) {
		reply(w, q, dns.RcodeSuccess)
	})
	r := newResolver(time.Second, f.roots(), f.server())
	_, err := r.Lookup(context.Background(), "empty.example.net")
	var de *net.DNSError
	if !errors.As(err, &de) || !de.IsNotFound {
		t.Fatalf("err %v", err)
	}
}

func TestLookupTimeout(t *testing.T) {
	f := newFakeDoH(t, func(w http.ResponseWriter, r *http.Request, q *dns.Msg) {
		<-r.Context().Done()
	})
	r := newResolver(200*time.Millisecond, f.roots(), f.server())
	start := time.Now()
	_, err := r.Lookup(context.Background(), "slow.example.net")
	var de *net.DNSError
	if !errors.As(err, &de) || !de.IsTimeout || de.IsNotFound {
		t.Fatalf("err %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

// Servers that fail, refuse or are not there do not stop one that answers.
func TestLookupFirstServerThatAnswers(t *testing.T) {
	broken := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, q *dns.Msg) {
		reply(w, q, dns.RcodeServerFailure)
	})
	bad := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, _ *dns.Msg) {
		http.Error(w, "nope", http.StatusInternalServerError)
	})
	good := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, q *dns.Msg) {
		reply(w, q, dns.RcodeSuccess, "ok.example.net. 60 IN A 198.51.100.3")
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := Server{Addr: netip.MustParseAddrPort(ln.Addr().String()), URL: "https://closed.example/dns-query"}
	ln.Close()
	roots := x509.NewCertPool()
	for _, f := range []*fakeDoH{broken, bad, good} {
		roots.AddCert(f.srv.Certificate())
	}
	// All of them share the name example.com: each is still dialled at its
	// own address.
	r := newResolver(2*time.Second, roots, closed, broken.server(), bad.server(), good.server())
	addrs, err := r.Lookup(context.Background(), "ok.example.net")
	if err != nil || len(addrs) != 1 || addrs[0] != netip.MustParseAddr("198.51.100.3") {
		t.Errorf("one answering: %v %v", addrs, err)
	}

	r = newResolver(2*time.Second, roots, closed, broken.server(), bad.server())
	_, err = r.Lookup(context.Background(), "ok.example.net")
	var de *net.DNSError
	if !errors.As(err, &de) || de.IsNotFound || de.IsTimeout {
		t.Fatalf("no server answering: %v", err)
	}
	for _, want := range []string{"SERVFAIL", "500"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %s", err, want)
		}
	}
}

// The certificate is checked against the server's name, not its address.
func TestLookupChecksCertificate(t *testing.T) {
	f := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, q *dns.Msg) {
		reply(w, q, dns.RcodeSuccess, "x.example.net. 60 IN A 192.0.2.1")
	})
	r := newResolver(time.Second, nil, f.server()) // the system's roots
	if _, err := r.Lookup(context.Background(), "x.example.net"); err == nil {
		t.Error("an unknown certificate was trusted")
	}
	s := f.server()
	s.URL = "https://other.example/dns-query"
	r = newResolver(time.Second, f.roots(), s)
	if _, err := r.Lookup(context.Background(), "x.example.net"); err == nil {
		t.Error("a certificate for another name was trusted")
	}
}

// Connections go to the fixed addresses through Dial, never to a lookup.
func TestLookupUsesDial(t *testing.T) {
	f := newFakeDoH(t, func(w http.ResponseWriter, _ *http.Request, q *dns.Msg) {
		reply(w, q, dns.RcodeSuccess, "d.example.net. 60 IN A 192.0.2.2")
	})
	var dialled []string
	r := newResolver(time.Second, f.roots(), f.server())
	r.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialled = append(dialled, addr)
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	if _, err := r.Lookup(context.Background(), "d.example.net"); err != nil {
		t.Fatal(err)
	}
	if len(dialled) != 1 || dialled[0] != f.server().Addr.String() {
		t.Errorf("dialled %v", dialled)
	}
}

func TestBadServerURL(t *testing.T) {
	r := &Resolver{Servers: []Server{{Addr: netip.MustParseAddrPort("192.0.2.1:443"), URL: "http://plain.example/dns-query"}}}
	if _, err := r.Lookup(context.Background(), "a.example.net"); err == nil {
		t.Error("plain HTTP accepted")
	}
}
