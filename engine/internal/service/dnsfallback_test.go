package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/dnsprobe"
	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/tunlayer"
)

const adguardDoH = "https://94.140.14.14/dns-query"

// The TUN layer's reports of a lookup the direct resolver could not answer,
// as the desktop (sing-box's own clock and colours) and a phone print them.
func exchangeFailedLine(i int, cause string) string {
	return fmt.Sprintf("ERROR[%04d] [%d 12ms] dns: exchange failed for site%d.example. IN A: %s", i, 1000+i, i, cause)
}

const (
	refusedCause = `Post "https://94.140.14.14/dns-query": dial tcp 94.140.14.14:443: connect: connection refused`
	resetCause   = `Post "https://94.140.14.14/dns-query": read tcp 10.0.0.5:51234->94.140.14.14:443: read: connection reset by peer`
	silentCause  = `context deadline exceeded`
)

func TestFailureLine(t *testing.T) {
	for _, c := range []struct {
		line  string
		host  string
		kind  dnsprobe.Kind
		named bool
		ok    bool
	}{
		{exchangeFailedLine(1, refusedCause), "94.140.14.14", dnsprobe.KindRefused, true, true},
		{exchangeFailedLine(2, resetCause), "94.140.14.14", dnsprobe.KindReset, true, true},
		{exchangeFailedLine(3, `dial TCP connection: dial tcp 94.140.14.14:853: i/o timeout`), "94.140.14.14", dnsprobe.KindTimeout, true, true},
		{exchangeFailedLine(4, `tls: failed to verify certificate: x509: certificate signed by unknown authority (94.140.14.14:443)`), "94.140.14.14", dnsprobe.KindTLS, true, true},
		// IPv6, in brackets.
		{exchangeFailedLine(5, `dial tcp [2a10:50c0::ad1:ff]:443: connect: network is unreachable`), "2a10:50c0::ad1:ff", dnsprobe.KindUnreachable, true, true},
		// A silence does not name its resolver, yet counts: it is looked at.
		{exchangeFailedLine(6, silentCause), "94.140.14.14", dnsprobe.KindTimeout, false, true},
		// Another resolver's failure (the network's, by its address) is not it...
		{exchangeFailedLine(7, `dial tcp 192.168.1.1:53: connect: connection refused`), "94.140.14.14", dnsprobe.KindRefused, false, false},
		// ...nor a prefix of its address.
		{exchangeFailedLine(8, `dial tcp 94.140.14.140:443: connect: connection refused`), "94.140.14.14", dnsprobe.KindRefused, false, false},
		// Other lines about names are not about resolvers.
		{"ERROR dns: lookup failed for x.example: dial tcp 94.140.14.14:443: connect: connection refused", "94.140.14.14", "", false, false},
		{"ERROR connection: open connection to 94.140.14.14:443 using outbound/direct[direct]: dial tcp 94.140.14.14:443: connect: connection refused", "94.140.14.14", "", false, false},
		{"INFO dns: exchanged A site.example. 60 IN A 192.0.2.1", "94.140.14.14", "", false, false},
	} {
		kind, named, ok := failureLine(c.line, c.host)
		if kind != c.kind || named != c.named || ok != c.ok {
			t.Errorf("failureLine(%q) = %q, %v, %v; want %q, %v, %v", c.line, kind, named, ok, c.kind, c.named, c.ok)
		}
	}
}

func TestDirectDNSWatchThreshold(t *testing.T) {
	t0 := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	var w directDNSWatch
	if w.failure(1, t0, dnsprobe.KindRefused) {
		t.Error("an unarmed watch looks")
	}
	w.arm(1, adguardDoH)
	if w.failure(2, t0, dnsprobe.KindRefused) {
		t.Error("a failure of another connection counts")
	}
	// Three within the window.
	if w.failure(1, t0, dnsprobe.KindRefused) || w.failure(1, t0.Add(10*time.Second), "") {
		t.Fatal("looks before three failures")
	}
	if !w.failure(1, t0.Add(20*time.Second), "") {
		t.Fatal("does not look at the third")
	}
	if w.lastKind != dnsprobe.KindRefused {
		t.Errorf("kind = %q: the line that said why is kept", w.lastKind)
	}
	// One look at a time, and after it found the resolver fine, quiet.
	if w.failure(1, t0.Add(21*time.Second), "") {
		t.Error("looks while looking")
	}
	w.mu.Lock()
	w.looking, w.quietUntil = false, t0.Add(20*time.Second+directDNSQuiet)
	w.mu.Unlock()
	for i := range 5 {
		if w.failure(1, t0.Add(25*time.Second+time.Duration(i)*time.Second), "") {
			t.Fatal("looks during the quiet")
		}
	}

	// Three spread over more than the window are not a burst.
	w.disarm()
	w.arm(1, adguardDoH)
	if w.failure(1, t0, "") || w.failure(1, t0.Add(20*time.Second), "") || w.failure(1, t0.Add(40*time.Second), "") {
		t.Error("failures 20 s apart made a burst")
	}

	// Disarmed, and a declared resolver is not looked at again.
	w.disarm()
	if w.failure(1, t0, "") || w.failure(1, t0, "") || w.failure(1, t0, "") {
		t.Error("a disarmed watch looks")
	}
}

// probeStub stands in for the probe of the user's resolver.
type probeStub struct {
	mu    sync.Mutex
	res   dnsprobe.Result
	skip  string
	calls []string // "direct" or "tunnel", with the server
}

func (p *probeStub) probe(_ context.Context, raw string, tunnel bool) (dnsprobe.Result, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	how := "direct"
	if tunnel {
		how = "tunnel"
	}
	p.calls = append(p.calls, how+" "+raw)
	return p.res, p.skip
}

func (p *probeStub) set(res dnsprobe.Result, skip string) {
	p.mu.Lock()
	p.res, p.skip = res, skip
	p.mu.Unlock()
}

func (p *probeStub) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func connectWithDirectDNS(t *testing.T, direct string, stub *probeStub) *harness {
	t.Helper()
	h := newHarness(t, func(c *Config) {
		c.DNS.Direct = direct
		c.dnsProbe = stub.probe
		c.dnsRecheck = 150 * time.Millisecond
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, Connected, 10*time.Second)
	return h
}

func dnsDirectEvents(events []Event) (out []Event) {
	for _, e := range events {
		if e.Kind == "dns-direct" {
			out = append(out, e)
		}
	}
	return out
}

// The user's own resolver gets the network's behind it; where the settings
// leave the choice to the system, the resolver is the network's already and
// nothing stands behind it.
func TestServiceDirectDNSFallbackOptions(t *testing.T) {
	h := connectWithDirectDNS(t, adguardDoH, &probeStub{})
	h.tun.mu.Lock()
	o := h.tun.opts
	h.tun.mu.Unlock()
	if o.DNS.Direct != adguardDoH {
		t.Errorf("direct = %q", o.DNS.Direct)
	}
	if want := []netip.Addr{netip.MustParseAddr("192.0.2.53")}; fmt.Sprint(o.DNS.DirectFallback) != fmt.Sprint(want) {
		t.Errorf("fallback = %v, want the network's resolvers %v", o.DNS.DirectFallback, want)
	}
	// And the config built from them has the chain.
	b, err := tunlayer.Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"action": "evaluate"`) || !strings.Contains(string(b), `"network1"`) {
		t.Errorf("no chain in the config:\n%s", b)
	}

	auto := newHarness(t, nil)
	if err := auto.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if o := auto.tun.opts; o.DNS.Direct != "192.0.2.53" || len(o.DNS.DirectFallback) != 0 {
		t.Errorf("system DNS: direct %q, fallback %v", o.DNS.Direct, o.DNS.DirectFallback)
	}
}

// Failed lookups pile up, the resolver is tried and found dead: one journal
// line says so, with the reason, and the status carries it for the home
// page. When it answers again, the note is taken back.
func TestServiceDirectDNSDownAndRestored(t *testing.T) {
	stub := &probeStub{res: dnsprobe.Result{Kind: dnsprobe.KindRefused}}
	h := connectWithDirectDNS(t, adguardDoH, stub)
	eventsFor(h, 100*time.Millisecond) // what the connection said

	for i := range 3 {
		h.svc.Log("tun", "\x1b[31m"+exchangeFailedLine(i, refusedCause)+"\x1b[0m")
	}
	got := eventsFor(h, 100*time.Millisecond)
	var down []Event
	for _, e := range dnsDirectEvents(got) {
		down = append(down, e)
	}
	if len(down) != 1 || down[0].Reason != "down" || down[0].Code != "dns.direct_down" || down[0].Args["addr"] != "94.140.14.14" {
		t.Fatalf("events: %+v", got)
	}
	why, _ := down[0].Args["why"].(msg.Msg)
	if why.Code != "dns.why.refused" {
		t.Errorf("why = %+v", down[0].Args["why"])
	}
	want := "DNS напрямую (94.140.14.14) недоступен в этой сети (соединение отклонено) — использую DNS сети"
	if down[0].Line != want {
		t.Errorf("line = %q, want %q", down[0].Line, want)
	}
	// The lookups' own lines are not in the journal.
	for _, e := range got {
		if e.Kind == "log" && strings.Contains(e.Line, "exchange failed") {
			t.Errorf("a line about the user's resolver in the journal: %+v", e)
		}
	}
	st := h.svc.Status()
	if st.DirectDNS != "94.140.14.14" || st.DirectDNSWhy == nil || st.DirectDNSWhy.Code != "dns.why.refused" {
		t.Errorf("status = %+v", st)
	}
	b, _ := json.Marshal(st)
	if !strings.Contains(string(b), `"direct_dns":"94.140.14.14"`) || !strings.Contains(string(b), `"direct_dns_why":{"code":"dns.why.refused"}`) {
		t.Errorf("status json %s", b)
	}

	// More failures: told once.
	for i := 3; i < 10; i++ {
		h.svc.Log("tun", exchangeFailedLine(i, refusedCause))
	}
	for _, e := range dnsDirectEvents(eventsFor(h, 100*time.Millisecond)) {
		if e.Reason == "down" {
			t.Errorf("told again: %+v", e)
		}
	}

	// It answers again.
	stub.set(dnsprobe.Result{OK: true, RTT: 30 * time.Millisecond}, "")
	var restored []Event
	deadline := time.Now().Add(5 * time.Second)
	for len(restored) == 0 && time.Now().Before(deadline) {
		restored = dnsDirectEvents(eventsFor(h, 200*time.Millisecond))
	}
	if len(restored) != 1 || restored[0].Reason != "restored" || restored[0].Code != "dns.direct_restored" || restored[0].Args["addr"] != "94.140.14.14" {
		t.Fatalf("restored: %+v", restored)
	}
	if st := h.svc.Status(); st.DirectDNS != "" || st.DirectDNSWhy != nil {
		t.Errorf("status after: %+v", st)
	}

	// It can fail again, and be told again.
	stub.set(dnsprobe.Result{Kind: dnsprobe.KindTimeout}, "")
	for i := 20; i < 23; i++ {
		h.svc.Log("tun", exchangeFailedLine(i, silentCause))
	}
	again := dnsDirectEvents(eventsFor(h, 300*time.Millisecond))
	if len(again) != 1 || again[0].Reason != "down" || !strings.Contains(again[0].Line, "(нет ответа)") {
		t.Errorf("second time: %+v", again)
	}
}

// A resolver that answers is not blamed for lookups that failed, and the
// failures are left alone for a while.
func TestServiceDirectDNSFineIsNotBlamed(t *testing.T) {
	stub := &probeStub{res: dnsprobe.Result{OK: true, RTT: 20 * time.Millisecond}}
	h := connectWithDirectDNS(t, adguardDoH, stub)
	eventsFor(h, 100*time.Millisecond)
	for i := range 3 {
		h.svc.Log("tun", exchangeFailedLine(i, silentCause))
	}
	time.Sleep(200 * time.Millisecond)
	for i := 3; i < 20; i++ {
		h.svc.Log("tun", exchangeFailedLine(i, silentCause))
	}
	if n := stub.count(); n != 1 {
		t.Errorf("the resolver was tried %d times: once, then quiet", n)
	}
	if ev := dnsDirectEvents(eventsFor(h, 200*time.Millisecond)); len(ev) != 0 || h.svc.Status().DirectDNS != "" {
		t.Errorf("a working resolver was blamed: %+v", ev)
	}
	// A silence is not named: those lines stay in the journal, grouped.
}

// A resolver that cannot be tried from here (QUIC) is judged by what the
// lookups said, if they named it.
func TestServiceDirectDNSUnsupportedKind(t *testing.T) {
	stub := &probeStub{res: dnsprobe.Result{Kind: dnsprobe.KindUnsupported}}
	h := connectWithDirectDNS(t, adguardDoH, stub)
	eventsFor(h, 100*time.Millisecond)
	for i := range 3 {
		h.svc.Log("tun", exchangeFailedLine(i, refusedCause))
	}
	ev := dnsDirectEvents(eventsFor(h, 200*time.Millisecond))
	if len(ev) != 1 || !strings.Contains(ev[0].Line, "(соединение отклонено)") {
		t.Errorf("events: %+v", ev)
	}
}

// With the system's resolver as the direct one there is nothing to fall back
// to: the lines are the journal's as before, and nothing is tried.
func TestServiceDirectDNSNotWatchedForSystemDNS(t *testing.T) {
	stub := &probeStub{res: dnsprobe.Result{Kind: dnsprobe.KindRefused}}
	h := connectWithDirectDNS(t, "", stub)
	eventsFor(h, 100*time.Millisecond)
	for i := range 6 {
		h.svc.Log("tun", exchangeFailedLine(i, refusedCause))
	}
	got := eventsFor(h, 200*time.Millisecond)
	if len(dnsDirectEvents(got)) != 0 || stub.count() != 0 {
		t.Errorf("watched the system DNS: %+v, %d probes", got, stub.count())
	}
	logged := 0
	for _, e := range got {
		if e.Kind == "log" {
			logged++
		}
	}
	if logged == 0 {
		t.Error("the lines were swallowed")
	}
}

// What was told does not outlive the connection.
func TestServiceDirectDNSEndsWithTheConnection(t *testing.T) {
	stub := &probeStub{res: dnsprobe.Result{Kind: dnsprobe.KindRefused}}
	h := connectWithDirectDNS(t, adguardDoH, stub)
	for i := range 3 {
		h.svc.Log("tun", exchangeFailedLine(i, refusedCause))
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.svc.Status().DirectDNS == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.svc.Status().DirectDNS == "" {
		t.Fatal("not told")
	}
	h.svc.Disconnect()
	if st := h.svc.Status(); st.DirectDNS != "" {
		t.Errorf("status after disconnect: %+v", st)
	}
	// The recheck loop is over: a resolver that answers now tells nobody.
	stub.set(dnsprobe.Result{OK: true}, "")
	n := stub.count()
	time.Sleep(400 * time.Millisecond)
	if stub.count() != n {
		t.Errorf("the recheck goes on after the connection")
	}
	for i := range 3 {
		h.svc.Log("tun", exchangeFailedLine(i, refusedCause))
	}
	if stub.count() != n {
		t.Error("lines after the connection are looked into")
	}
}
