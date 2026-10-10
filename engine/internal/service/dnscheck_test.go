package service

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/dnsprobe"
	"coreshift/engine/internal/ping"

	"github.com/miekg/dns"
)

func TestLogShapeGroupsLookupsOfDifferentNames(t *testing.T) {
	a := logShape(tidy(exchangeFailedLine(1, refusedCause)))
	b := logShape(tidy(exchangeFailedLine(2, refusedCause)))
	if a == "" || a != b || !strings.Contains(a, "exchange failed for <имя>") {
		t.Errorf("shapes %q, %q", a, b)
	}
	if logShape(tidy(exchangeFailedLine(1, silentCause))) == a {
		t.Error("a refusal and a silence are one line")
	}
}

func TestDNSCheckItemWords(t *testing.T) {
	for _, c := range []struct {
		name   string
		res    dnsprobe.Result
		skip   string
		tunnel bool
		status string
		code   string
		line   string
	}{
		{"answers", dnsprobe.Result{OK: true, RTT: 35 * time.Millisecond}, "", false, DNSCheckOK, "dnscheck.ok", "отвечает (35 мс)"},
		{"answers at once", dnsprobe.Result{OK: true}, "", false, DNSCheckOK, "dnscheck.ok", "отвечает (1 мс)"},
		{"refused", dnsprobe.Result{Kind: dnsprobe.KindRefused}, "", false, DNSCheckFail, "dnscheck.blocked",
			"не отвечает в этой сети: соединение отклонено — провайдер, похоже, блокирует этот DNS"},
		{"silent", dnsprobe.Result{Kind: dnsprobe.KindTimeout}, "", false, DNSCheckFail, "dnscheck.blocked",
			"не отвечает в этой сети: нет ответа — провайдер, похоже, блокирует этот DNS"},
		{"unreachable", dnsprobe.Result{Kind: dnsprobe.KindUnreachable}, "", false, DNSCheckFail, "dnscheck.fail",
			"не отвечает в этой сети: сеть не достигает этого адреса"},
		{"says no", dnsprobe.Result{Kind: dnsprobe.KindRcode, Err: "REFUSED"}, "", false, DNSCheckFail, "dnscheck.fail",
			"не отвечает в этой сети: ответил ошибкой REFUSED"},
		{"odd", dnsprobe.Result{Kind: dnsprobe.KindOther, Err: "weird"}, "", false, DNSCheckFail, "dnscheck.fail", "не отвечает в этой сети: weird"},
		{"tunnel", dnsprobe.Result{Kind: dnsprobe.KindTimeout}, "", true, DNSCheckFail, "dnscheck.remote_fail", "не отвечает через VPN: нет ответа"},
		{"not connected", dnsprobe.Result{}, skipNotConnected, true, DNSCheckSkipped, "dnscheck.skip.not_connected", "через VPN проверю, когда он подключён"},
		{"no way around", dnsprobe.Result{}, skipNoPhysical, false, DNSCheckSkipped, "dnscheck.skip.no_physical", "не удалось выйти в сеть мимо VPN для проверки"},
		{"quic", dnsprobe.Result{Kind: dnsprobe.KindUnsupported}, "", false, DNSCheckSkipped, "dnscheck.skip.unsupported", "этот вид DNS (QUIC, HTTP/3) не проверяется"},
	} {
		item := dnsCheckItem(c.res, c.skip, c.tunnel)
		if item.Status != c.status || item.Code != c.code || item.Line != c.line {
			t.Errorf("%s: %+v", c.name, item)
		}
	}
	// A failure carries what it was, for the app to tell apart.
	if item := dnsCheckItem(dnsprobe.Result{Kind: dnsprobe.KindRefused}, "", false); item.Kind != "refused" {
		t.Errorf("kind = %q", item.Kind)
	}
}

// Every reason has words in the catalog.
func TestWhyHasWordsForEveryKind(t *testing.T) {
	for _, k := range []dnsprobe.Kind{
		dnsprobe.KindRefused, dnsprobe.KindTimeout, dnsprobe.KindTLS, dnsprobe.KindReset, dnsprobe.KindUnreachable,
		dnsprobe.KindRcode, dnsprobe.KindHTTP, dnsprobe.KindOther,
	} {
		m := whyMsg(k, "x")
		if s := m.String(); s == "" || strings.Contains(s, "{") || strings.Contains(s, m.Code+"}") {
			t.Errorf("%s: %q", k, s)
		}
	}
}

func TestCheckDNSValidation(t *testing.T) {
	h := newHarness(t, nil)
	for _, c := range []struct{ direct, remote string }{
		{"dns.example", ""},         // the direct one is an address, as the TUN layer needs
		{"ftp://1.1.1.1", ""},       // not a DNS scheme
		{"", "gopher://1.1.1.1"},    // the same for the other
		{"1.1.1.1:99999", ""},       // not a port
		{"https://", ""},            // no host
		{"192.0.2.1", "udp://:853"}, // the second is bad
	} {
		if _, err := h.svc.CheckDNS(context.Background(), DNSCheckRequest{Direct: c.direct, Remote: c.remote}); err == nil {
			t.Errorf("%+v was taken", c)
		}
	}
	// A hostname is fine for the resolver in the tunnel.
	h.svc.cfg.dnsProbe = func(context.Context, string, bool) (dnsprobe.Result, string) {
		return dnsprobe.Result{}, skipNotConnected
	}
	if res, err := h.svc.CheckDNS(context.Background(), DNSCheckRequest{Remote: "tls://dns.google"}); err != nil || res.Remote == nil || res.Direct != nil {
		t.Errorf("remote hostname: %+v, %v", res, err)
	}
	if res, err := h.svc.CheckDNS(context.Background(), DNSCheckRequest{}); err != nil || res.Remote != nil || res.Direct != nil {
		t.Errorf("nothing asked: %+v, %v", res, err)
	}
}

// The way each server is reached: the direct one around the tunnel, the one
// that lives in the tunnel through it.
func TestCheckDNSWays(t *testing.T) {
	stub := &probeStub{res: dnsprobe.Result{OK: true, RTT: 12 * time.Millisecond}}
	h := newHarness(t, func(c *Config) { c.dnsProbe = stub.probe })
	res, err := h.svc.CheckDNS(context.Background(), DNSCheckRequest{Direct: adguardDoH, Remote: "https://1.1.1.1/dns-query"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Direct == nil || res.Remote == nil || res.Direct.Status != DNSCheckOK || res.Remote.Status != DNSCheckOK {
		t.Fatalf("result = %+v %+v", res.Direct, res.Remote)
	}
	stub.mu.Lock()
	calls := append([]string(nil), stub.calls...)
	stub.mu.Unlock()
	if len(calls) != 2 || !strings.Contains(strings.Join(calls, "|"), "direct "+adguardDoH) || !strings.Contains(strings.Join(calls, "|"), "tunnel https://1.1.1.1/dns-query") {
		t.Errorf("calls = %v", calls)
	}
}

// loopbackDNS is a resolver on loopback answering every query.
func loopbackDNS(t *testing.T, rcode int) (udp, tcp string) {
	t.Helper()
	answer := func(wire []byte) []byte {
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
			pc.WriteTo(answer(buf[:n]), from)
		}
	}()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
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
				a := answer(wire)
				c.Write(binary.BigEndian.AppendUint16(nil, uint16(len(a))))
				c.Write(a)
			}()
		}
	}()
	return pc.LocalAddr().String(), l.Addr().String()
}

// The real probe, end to end, against resolvers on loopback: an answering
// one, a dead port, and the tunnel one while nothing is connected.
func TestCheckDNSRealProbe(t *testing.T) {
	udp, tcp := loopbackDNS(t, dns.RcodeSuccess)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := l.Addr().String()
	l.Close()

	h := newHarness(t, nil)
	res, err := h.svc.CheckDNS(context.Background(), DNSCheckRequest{Direct: "udp://" + udp, Remote: "tcp://" + tcp})
	if err != nil {
		t.Fatal(err)
	}
	if res.Direct.Status != DNSCheckOK || !strings.HasPrefix(res.Direct.Line, "отвечает (") {
		t.Errorf("direct = %+v", res.Direct)
	}
	// Idle: nothing carries the resolver of the tunnel.
	if res.Remote.Status != DNSCheckSkipped || res.Remote.Kind != "not_connected" {
		t.Errorf("remote = %+v", res.Remote)
	}

	res, err = h.svc.CheckDNS(context.Background(), DNSCheckRequest{Direct: "tcp://" + dead})
	if err != nil {
		t.Fatal(err)
	}
	if res.Direct.Status != DNSCheckFail || res.Direct.Kind != "refused" || res.Direct.Code != "dnscheck.blocked" ||
		res.Direct.Line != "не отвечает в этой сети: соединение отклонено — провайдер, похоже, блокирует этот DNS" {
		t.Errorf("dead = %+v", res.Direct)
	}
}

// While the tunnel is up the direct resolver is reached around it, from the
// physical interface; where that cannot be had, it is not tried at all
// rather than tried through the tunnel, which would say nothing of the
// network.
func TestCheckDNSDirectNeedsThePhysicalInterface(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, Connected, 10*time.Second)
	// The harness has no physical network: nothing to leave by.
	res, err := h.svc.CheckDNS(context.Background(), DNSCheckRequest{Direct: "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Direct.Status != DNSCheckSkipped || res.Direct.Kind != "no_physical" {
		t.Errorf("direct = %+v", res.Direct)
	}
	// With it, the probe leaves bound to it.
	h.svc.cfg.physical = func() (ping.Bind, error) { return ping.Bind{Source: netip.MustParseAddr("192.0.2.77")}, nil }
	dial, ok := h.svc.physicalDial("192.0.2.1")
	if !ok || dial == nil {
		t.Error("no way around with a physical interface")
	}
	// An address of the machine itself needs none.
	h.svc.cfg.physical = func() (ping.Bind, error) { return ping.Bind{}, nil }
	if _, ok := h.svc.physicalDial("127.0.0.1"); !ok {
		t.Error("loopback refused")
	}
	// Where the platform keeps the app outside its VPN, as Android does,
	// the system's own way is the physical one.
	h.svc.cfg.AppOutsideVPN = true
	if _, ok := h.svc.physicalDial("192.0.2.1"); !ok {
		t.Error("Android refused")
	}
}

func TestAPIDNSCheck(t *testing.T) {
	h, srv := newAPIServer(t)
	udp, _ := loopbackDNS(t, dns.RcodeSuccess)
	_ = h

	code, body := call(t, srv, "POST", "/v1/dnscheck", `{"direct":"udp://`+udp+`","remote":"tls://dns.example"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	direct, _ := body["direct"].(map[string]any)
	remote, _ := body["remote"].(map[string]any)
	if direct["status"] != "ok" || direct["code"] != "dnscheck.ok" || !strings.HasPrefix(direct["line"].(string), "отвечает (") {
		t.Errorf("direct = %v", direct)
	}
	if remote["status"] != "skipped" || remote["kind"] != "not_connected" || remote["code"] != "dnscheck.skip.not_connected" {
		t.Errorf("remote = %v", remote)
	}

	// What is not asked about is not answered.
	_, body = call(t, srv, "POST", "/v1/dnscheck", `{"direct":"udp://`+udp+`"}`)
	if _, has := body["remote"]; has || body["direct"] == nil {
		t.Errorf("only direct: %v", body)
	}

	// Not addresses the settings take.
	for _, bad := range []string{`{"direct":"dns.example"}`, `{"remote":"ftp://x"}`, `not json`} {
		if code, _ := call(t, srv, "POST", "/v1/dnscheck", bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, code)
		}
	}
	// A wrong method.
	if code, _ := call(t, srv, "GET", "/v1/dnscheck", ""); code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", code)
	}
}

// The endpoint dials where it is told, so it is as guarded as the rest:
// the token, and the Host that stops a web page from reaching it through
// DNS rebinding.
func TestAPIDNSCheckIsGuarded(t *testing.T) {
	_, srv := newAPIServer(t)
	udp, _ := loopbackDNS(t, dns.RcodeSuccess)
	body := `{"direct":"udp://` + udp + `"}`
	if code, out := call(t, srv, "POST", "/v1/dnscheck", body, func(r *http.Request) { r.Header.Del("Authorization") }); code != http.StatusUnauthorized || out["direct"] != nil {
		t.Errorf("no token: %d %v", code, out)
	}
	if code, out := call(t, srv, "POST", "/v1/dnscheck", body, func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }); code != http.StatusUnauthorized || out["direct"] != nil {
		t.Errorf("wrong token: %d %v", code, out)
	}
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	if code, out := call(t, srv, "POST", "/v1/dnscheck", body, func(r *http.Request) { r.Host = "evil.example:" + port }); code != http.StatusForbidden || out["direct"] != nil {
		t.Errorf("foreign host: %d %v", code, out)
	}
	// The body is small.
	if code, _ := call(t, srv, "POST", "/v1/dnscheck", `{"direct":"`+strings.Repeat("1", 70<<10)+`"}`); code != http.StatusRequestEntityTooLarge && code != http.StatusBadRequest {
		t.Errorf("a large body: %d", code)
	}
}
