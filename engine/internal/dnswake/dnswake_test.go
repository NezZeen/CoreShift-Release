package dnswake

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	stdlog "log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/include"
	sblog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"

	mDNS "github.com/miekg/dns"
)

func TestStale(t *testing.T) {
	for _, c := range []struct {
		wall, awake time.Duration
		want        bool
	}{
		{5 * time.Second, 5 * time.Second, false},
		{3 * time.Minute, 3 * time.Minute, false},  // idle a while, awake all along
		{4 * time.Minute, 4 * time.Minute, true},   // idle long enough for NATs and servers to drop it
		{13 * time.Minute, 80 * time.Second, true}, // the reported log: 80 s awake in 13 min
		{40 * time.Second, 5 * time.Second, true},  // slept 35 s
		{20 * time.Second, 5 * time.Second, false}, // a short nap
	} {
		if got := stale(c.wall, c.awake); got != c.want {
			t.Errorf("stale(wall %v, awake %v) = %v, want %v", c.wall, c.awake, got, c.want)
		}
	}
}

// The wrapped constructors must take the option types sing-box registers,
// or creating a transport panics on the type assertion inside.
func TestOptionTypesMatchSingBox(t *testing.T) {
	original := include.DNSTransportRegistry()
	wrapped := dns.NewTransportRegistry()
	wrapRegistry(wrapped, original)
	types := wrapped.OptionTypes()
	if len(types) != 6 {
		t.Errorf("wrapped types = %v", types)
	}
	for _, typ := range types {
		want, ok := original.CreateOptions(typ)
		got, _ := wrapped.CreateOptions(typ)
		if !ok || reflect.TypeOf(got) != reflect.TypeOf(want) {
			t.Errorf("%s: options %T, sing-box registers %T", typ, got, want)
		}
	}
}

func TestInstall(t *testing.T) {
	ctx := include.Context(context.Background())
	if err := Install(ctx); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background()); err == nil {
		t.Error("Install without registries succeeded")
	}
}

func TestInstalledConstructorWraps(t *testing.T) {
	registry := dns.NewTransportRegistry()
	wrapRegistry(registry, fakeRegistry{})
	got, err := registry.CreateDNSTransport(context.Background(), sblog.NewNOPFactory().NewLogger("dns"), "remote", C.DNSTypeHTTPS, &option.RemoteHTTPSDNSServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if w, ok := got.(*Transport); !ok || w.DNSTransport.Tag() != "remote" {
		t.Errorf("created %T", got)
	}
}

type fakeRegistry struct{ adapter.DNSTransportRegistry }

func (fakeRegistry) CreateDNSTransport(_ context.Context, _ sblog.ContextLogger, tag, typ string, _ any) (adapter.DNSTransport, error) {
	return &stub{TransportAdapter: dns.NewTransportAdapter(typ, tag, nil)}, nil
}

// stub answers every query, after failing the first fail ones as a closed
// connection would.
type stub struct {
	dns.TransportAdapter
	resets atomic.Int32
	fail   atomic.Int32
	// err is the failure, if not net.ErrClosed.
	err error
}

func (s *stub) Start(adapter.StartStage) error { return nil }
func (s *stub) Close() error                   { return nil }
func (s *stub) Reset()                         { s.resets.Add(1) }

func (s *stub) Exchange(_ context.Context, m *mDNS.Msg) (*mDNS.Msg, error) {
	if s.fail.Add(-1) >= 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, net.ErrClosed
	}
	r := new(mDNS.Msg)
	r.SetReply(m)
	return r, nil
}

func (s *stub) ExchangeAsync(ctx context.Context, m *mDNS.Msg, callback func(*mDNS.Msg, error)) {
	go func() { callback(s.Exchange(ctx, m)) }()
}

// sleep makes w see the device sleep for ten minutes after its last
// answer: elapsed adds the sleep to the wall clock for any time before now.
func sleep(w *Transport) {
	w.last = w.last.Add(-time.Millisecond) // before wake even on a coarse clock
	wake := time.Now()
	w.elapsed = func(last time.Time) (time.Duration, time.Duration) {
		awake := time.Since(last)
		if last.Before(wake) {
			return awake + 10*time.Minute, awake
		}
		return awake, awake
	}
}

func query() *mDNS.Msg {
	m := new(mDNS.Msg)
	m.SetQuestion("example.com.", mDNS.TypeA)
	return m
}

func TestRenewsOnceAfterSleep(t *testing.T) {
	s := &stub{}
	w := Wrap(s)
	ctx := context.Background()
	if _, err := w.Exchange(ctx, query()); err != nil || s.resets.Load() != 0 {
		t.Fatalf("awake: err %v, %d resets", err, s.resets.Load())
	}

	sleep(w)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := w.Exchange(ctx, query()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := s.resets.Load(); n != 1 {
		t.Errorf("%d renewals after one sleep, want 1", n)
	}
}

func TestRetriesClosedConnectionOnce(t *testing.T) {
	ctx := context.Background()
	s := &stub{}
	w := Wrap(s)
	s.fail.Store(1)
	if _, err := w.Exchange(ctx, query()); err != nil {
		t.Errorf("Exchange after a closed connection: %v", err)
	}
	s.fail.Store(1)
	done := make(chan error, 1)
	w.ExchangeAsync(ctx, query(), func(_ *mDNS.Msg, err error) { done <- err })
	if err := <-done; err != nil {
		t.Errorf("ExchangeAsync after a closed connection: %v", err)
	}
	s.fail.Store(2)
	if _, err := w.Exchange(ctx, query()); !errors.Is(err, net.ErrClosed) {
		t.Errorf("second failure: %v, want it returned", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	s.fail.Store(1)
	if _, err := w.Exchange(cancelled, query()); !errors.Is(err, net.ErrClosed) {
		t.Errorf("retried for a caller that gave up: %v", err)
	}
}

// A TLS connection garbled on the way ("bad record MAC", as reported on a
// phone's Wi-Fi): the connections are renewed once, however many lookups
// failed on it, and each is asked again on a fresh one. Other failures
// renew nothing.
func TestRenewsCorruptedTLSConnection(t *testing.T) {
	ctx := context.Background()
	s := &stub{err: &net.OpError{Op: "local error", Err: errors.New("tls: bad record MAC")}}
	w := Wrap(s)
	s.fail.Store(1)
	if _, err := w.Exchange(ctx, query()); err != nil {
		t.Errorf("Exchange after a corrupted connection: %v", err)
	}
	if n := s.resets.Load(); n != 1 {
		t.Errorf("%d renewals, want 1", n)
	}

	s.fail.Store(1)
	done := make(chan error, 1)
	w.ExchangeAsync(ctx, query(), func(_ *mDNS.Msg, err error) { done <- err })
	if err := <-done; err != nil || s.resets.Load() != 2 {
		t.Errorf("ExchangeAsync after a corrupted connection: err %v, %d renewals in all", err, s.resets.Load())
	}

	// Lookups that started before a renewal and failed together renew once.
	start := time.Now()
	time.Sleep(time.Millisecond) // a renewal after start even on a coarse clock
	for range 3 {
		w.renewIfBroken(s.err, start)
	}
	if n := s.resets.Load(); n != 3 {
		t.Errorf("%d renewals in all, want 3", n)
	}

	s.resets.Store(0)
	s.err = errors.New("dial tcp: connection refused")
	s.fail.Store(1)
	if _, err := w.Exchange(ctx, query()); err != nil || s.resets.Load() != 0 {
		t.Errorf("another failure: err %v, %d renewals", err, s.resets.Load())
	}
}

// The reported failure, reproduced with sing-box's own DoH transport: a
// DNS-over-HTTPS server reached through a relay standing in for the proxy
// path, whose open connections go silent (dropped by a NAT while the phone
// slept) while new ones still work.

type doh struct {
	transport *transport.HTTPSTransport
	relay     *relay
}

func newDoH(t *testing.T) *doh {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var q mDNS.Msg
		if q.Unpack(body) != nil || len(q.Question) != 1 {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		resp := new(mDNS.Msg)
		resp.SetReply(&q)
		resp.Answer = append(resp.Answer, &mDNS.A{
			Hdr: mDNS.RR_Header{Name: q.Question[0].Name, Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 60},
			A:   net.IPv4(192, 0, 2, 1),
		})
		b, _ := resp.Pack()
		w.Header().Set("Content-Type", transport.MimeType)
		w.Write(b)
	}))
	srv.EnableHTTP2 = true
	srv.Config.ErrorLog = stdlog.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	r := newRelay(t, srv.Listener.Addr().String())
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	d := &doh{relay: r}
	d.transport = transport.NewHTTPSRaw(
		dns.NewTransportAdapter(C.DNSTypeHTTPS, "remote", nil),
		sblog.NewNOPFactory().NewLogger("dns"),
		tlsDialer{addr: r.ln.Addr().String(), roots: roots},
		&url.URL{Scheme: "https", Host: "example.com", Path: "/dns-query"},
		http.Header{},
		M.ParseSocksaddr("127.0.0.1:443"),
		nil,
	)
	t.Cleanup(func() { d.transport.Close() })
	return d
}

// tlsDialer is the proxy path: a TCP connection through the relay, then TLS
// to the DoH server, as sing-box's TLS dialer would do over the detour.
type tlsDialer struct {
	addr  string
	roots *x509.CertPool
}

func (d tlsDialer) DialContext(ctx context.Context, _ string, _ M.Socksaddr) (net.Conn, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", d.addr)
	if err != nil {
		return nil, err
	}
	tc := tls.Client(c, &tls.Config{RootCAs: d.roots, ServerName: "example.com", NextProtos: []string{"h2"}})
	if err := tc.HandshakeContext(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return tc, nil
}

func (tlsDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.ErrUnsupported
}

// relay forwards connections to target. silence makes the open ones drop
// everything in both directions without closing, as a connection whose NAT
// mapping or server side is gone looks from the phone.
type relay struct {
	ln     net.Listener
	target string
	mu     sync.Mutex
	links  []*link
}

type link struct {
	a, b   net.Conn
	silent atomic.Bool
}

func newRelay(t *testing.T, target string) *relay {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &relay{ln: ln, target: target}
	go r.serve()
	t.Cleanup(r.close)
	return r
}

func (r *relay) serve() {
	for {
		a, err := r.ln.Accept()
		if err != nil {
			return
		}
		b, err := net.Dial("tcp", r.target)
		if err != nil {
			a.Close()
			continue
		}
		l := &link{a: a, b: b}
		r.mu.Lock()
		r.links = append(r.links, l)
		r.mu.Unlock()
		go l.pipe(a, b)
		go l.pipe(b, a)
	}
}

func (l *link) pipe(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if err != nil {
			l.a.Close()
			l.b.Close()
			return
		}
		if !l.silent.Load() {
			dst.Write(buf[:n])
		}
	}
}

func (r *relay) silence() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.links {
		l.silent.Store(true)
	}
}

func (r *relay) close() {
	r.ln.Close()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.links {
		l.a.Close()
		l.b.Close()
	}
}

func lookup(t *testing.T, tr adapter.DNSTransport, timeout time.Duration) (time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	resp, err := tr.Exchange(ctx, query())
	if err == nil && len(resp.Answer) != 1 {
		err = errors.New("no answer")
	}
	return time.Since(start), err
}

// Without the wrapper a lookup on the silent connection waits out its whole
// timeout; sing-box replaces the connection only then.
func TestSilentConnectionHangsUntilTimeout(t *testing.T) {
	d := newDoH(t)
	if _, err := lookup(t, d.transport, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	d.relay.silence()
	took, err := lookup(t, d.transport, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) || took < time.Second {
		t.Fatalf("lookup on the silent connection: %v after %v, want the timeout", err, took)
	}
	if took, err := lookup(t, d.transport, 5*time.Second); err != nil || took > time.Second {
		t.Errorf("after the timeout: %v after %v, want a fresh connection", err, took)
	}
}

// With it, the first lookup after the device slept goes out on a fresh
// connection at once.
func TestFreshConnectionAfterSleep(t *testing.T) {
	d := newDoH(t)
	w := Wrap(d.transport)
	if _, err := lookup(t, w, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	d.relay.silence()
	sleep(w)
	took, err := lookup(t, w, 10*time.Second)
	if err != nil || took > time.Second {
		t.Errorf("first lookup after sleep: %v after %v", err, took)
	}
}

// A lookup whose connection is closed under it (here a network change)
// is asked again on a fresh one instead of failing.
func TestLookupSurvivesReset(t *testing.T) {
	d := newDoH(t)
	w := Wrap(d.transport)
	if _, err := lookup(t, w, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	d.relay.silence()
	time.AfterFunc(300*time.Millisecond, w.Reset)
	took, err := lookup(t, w, 10*time.Second)
	if err != nil || took > 2*time.Second {
		t.Errorf("lookup across a reset: %v after %v", err, took)
	}
}
