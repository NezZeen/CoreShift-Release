package service

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/msg"
)

// The TUN layer's reports, as seen on a phone in a "white list" network and
// on Windows with Russian sites out of reach.
func refusedLine(i int) string {
	return fmt.Sprintf("ERROR [%d 45ms] connection: open connection to 95.163.%d.1:443 using outbound/direct[direct]: dial tcp 95.163.%d.1:443: connect: connection refused", 1000+i, i%10, i%10)
}

func timeoutLine(i int) string {
	return fmt.Sprintf("ERROR [%d 5.01s] connection: open connection to 87.250.%d.3:443 using outbound/direct[direct]: dial tcp 87.250.%d.3:443: i/o timeout", 2000+i, i%10, i%10)
}

func proxyFailLine(i int) string {
	return fmt.Sprintf("ERROR [%d 2.1s] connection: open connection to 104.16.%d.1:443 using outbound/socks[proxy]: dial tcp 127.0.0.1:1080: i/o timeout", 3000+i, i%10)
}

func TestDialFailure(t *testing.T) {
	for _, c := range []struct {
		line   string
		addr   string
		direct bool
		ok     bool
	}{
		{refusedLine(3), "95.163.3.1:443", true, true},
		{timeoutLine(4), "87.250.4.3:443", true, true},
		{"connection: open connection to 8.8.4.4:443 using outbound/direct[direct]: dial tcp 8.8.4.4:443: connectex: No connection could be made because the target machine actively refused it.", "8.8.4.4:443", true, true},
		{"ERROR outbound/direct[direct]: dial tcp 10.1.2.3:443: connect: network is unreachable", "10.1.2.3:443", true, true},
		{proxyFailLine(1), "104.16.1.1:443", false, true},
		// Not the network refusing the connection.
		{"ERROR connection: open connection to 1.2.3.4:443 using outbound/direct[direct]: EOF", "", false, false},
		{"ERROR dns: lookup failed for x.example: i/o timeout", "", false, false},
		{"INFO inbound/tun[tun-in]: inbound connection to 1.2.3.4:443", "", false, false},
	} {
		addr, direct, ok := dialFailure(c.line)
		if addr != c.addr || direct != c.direct || ok != c.ok {
			t.Errorf("dialFailure(%q) = %q, %v, %v; want %q, %v, %v", c.line, addr, direct, ok, c.addr, c.direct, c.ok)
		}
	}
}

// feed gives w the lines as the TUN layer would, one every step from t0.
func feed(w *directWatch, gen int, t0 time.Time, step time.Duration, lines ...string) time.Time {
	at := t0
	for _, l := range lines {
		addr, direct, ok := dialFailure(l)
		if ok {
			w.fail(gen, at, addr, direct)
		}
		at = at.Add(step)
	}
	return at
}

func lines(n int, f func(int) string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = f(i)
	}
	return out
}

func TestDirectWatchBurstHintsOnce(t *testing.T) {
	var w directWatch
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	at := feed(&w, 1, t0, 50*time.Millisecond, lines(200, refusedLine)...)
	n, due, loud := w.checked(1, at, true)
	if !due || !loud || n != 200 {
		t.Fatalf("checked = %d, %v, %v; want 200, true, true", n, due, loud)
	}
	// More of the same in the same connection: told already.
	at = feed(&w, 1, at, 50*time.Millisecond, lines(200, refusedLine)...)
	if _, due, _ := w.checked(1, at, true); due {
		t.Fatal("the hint came twice in one connection")
	}
	// The next connection may say it again, but the journal stays quiet
	// for directRepeat.
	at = feed(&w, 2, at.Add(time.Minute), 100*time.Millisecond, lines(30, timeoutLine)...)
	if _, due, loud := w.checked(2, at, true); !due || loud {
		t.Fatalf("next connection: due %v loud %v; want true, false", due, loud)
	}
	at = feed(&w, 3, at.Add(directRepeat), 100*time.Millisecond, lines(30, timeoutLine)...)
	if _, due, loud := w.checked(3, at, true); !due || !loud {
		t.Fatalf("after %v: due %v loud %v; want true, true", directRepeat, due, loud)
	}
}

func TestDirectWatchFewFailures(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for name, ls := range map[string][]string{
		"few":           lines(directFails-1, refusedLine),
		"one site":      lines(100, func(int) string { return refusedLine(0) }),
		"spread":        lines(40, timeoutLine), // one in 5 s: 12 a minute
		"proxied too":   append(lines(20, refusedLine), lines(10, proxyFailLine)...),
		"proxied alone": lines(50, proxyFailLine),
	} {
		var w directWatch
		step := 50 * time.Millisecond
		if name == "spread" {
			step = 5 * time.Second
		}
		at := feed(&w, 1, t0, step, ls...)
		if n, due, _ := w.checked(1, at, true); due {
			t.Errorf("%s: hint after %d failures", name, n)
		}
	}
}

// A check through the core that fails means the network or the server is
// at fault, not what the settings send direct.
func TestDirectWatchCoreFailing(t *testing.T) {
	var w directWatch
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	at := feed(&w, 1, t0, 50*time.Millisecond, lines(100, refusedLine)...)
	if _, due, _ := w.checked(1, at, false); due {
		t.Fatal("hint on a failed check")
	}
	if _, due, _ := w.checked(1, at.Add(time.Second), true); due {
		t.Fatal("hint after a failed check, from failures before it")
	}
}

func TestDirectRoutesAndHint(t *testing.T) {
	ru := Options{TUN: true, DNS: DNSSettings{RussiaDirect: true}}
	sel := Options{TUN: true, Selective: true, DNS: DNSSettings{RussiaDirect: true}}
	lists := Options{TUN: true, DirectIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}}
	for _, c := range []struct {
		o      Options
		advice string   // the advice's code
		want   []string // in the hint's Russian
		not    []string
	}{
		{ru, "direct.advice.russia", []string{"(17 за минуту)", "Выключите «Российские сайты напрямую»."}, []string{"Всё через VPN", "списк"}},
		{sel, "direct.advice.all_russia", []string{"Включите «Всё через VPN» и выключите «Российские сайты напрямую»."}, nil},
		{lists, "direct.advice.lists", []string{"Уберите из своих списков «напрямую»"}, []string{"Российские"}},
		{Options{TUN: true, DNS: DNSSettings{RussiaDirect: true, DirectSuffixes: []string{"bank.example"}}}, "direct.advice.russia_lists",
			[]string{"Выключите «Российские сайты напрямую»; проверьте и свои списки"}, nil},
		{Options{TUN: true}, "direct.advice.none", []string{"отсюда недоступны."}, []string{"  ", "недоступны. "}},
	} {
		m := directHint(17, directRoutes(c.o))
		if m.Code != "direct.blocked" || m.Args["n"] != 17 {
			t.Errorf("hint = %+v", m)
		}
		if a, _ := m.Args["advice"].(msg.Msg); a.Code != c.advice || !msg.Has(a.Code) {
			t.Errorf("advice = %+v, want %s", m.Args["advice"], c.advice)
		}
		h := m.String()
		if strings.HasSuffix(h, " ") {
			t.Errorf("hint %q ends in a space", h)
		}
		for _, w := range c.want {
			if !strings.Contains(h, w) {
				t.Errorf("hint %q lacks %q", h, w)
			}
		}
		for _, w := range c.not {
			if strings.Contains(h, w) {
				t.Errorf("hint %q has %q", h, w)
			}
		}
	}
	if r := directRoutes(Options{TUN: true}); len(r) != 0 {
		t.Errorf("all through the VPN: routes %v", r)
	}
	if r := directRoutes(Options{DNS: DNSSettings{RussiaDirect: true}}); len(r) != 0 {
		t.Errorf("without TUN: routes %v", r)
	}
}

// The service: the TUN layer's refusals while the checks through the core
// pass give one "direct" event and the status flag; with nothing sent
// direct, nothing.
func TestServiceDirectBlocked(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.DNS.DirectSuffixes = []string{"bank.example"} })
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, Connected, 10*time.Second)
	waitHealthy(t, h)
	for _, l := range lines(60, refusedLine) {
		h.svc.Log("tun", "\x1b[31m"+l+"\x1b[0m")
	}
	var got []Event
	for _, e := range eventsFor(h, 2*time.Second) {
		if e.Kind == "direct" {
			got = append(got, e)
		}
	}
	if len(got) != 1 || got[0].Code != "direct.blocked" || got[0].Args["n"] != 60 {
		t.Fatalf("direct events: %+v", got)
	}
	// The Russian for older apps and the journal.
	if !strings.Contains(got[0].Line, "Прямые соединения не проходят (60 за минуту)") || !strings.Contains(got[0].Line, "Уберите") {
		t.Errorf("fallback text: %q", got[0].Line)
	}
	if !h.svc.Status().DirectBlocked {
		t.Error("status has no direct_blocked")
	}
	for _, l := range lines(60, refusedLine) {
		h.svc.Log("tun", l)
	}
	for _, e := range eventsFor(h, time.Second) {
		if e.Kind == "direct" {
			t.Fatalf("second direct event: %+v", e)
		}
	}
}

func TestServiceDirectNothingDirect(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, Connected, 10*time.Second)
	waitHealthy(t, h)
	for _, l := range lines(60, refusedLine) {
		h.svc.Log("tun", l)
	}
	for _, e := range eventsFor(h, time.Second) {
		if e.Kind == "direct" {
			t.Fatalf("direct event with nothing sent direct: %+v", e)
		}
	}
	if h.svc.Status().DirectBlocked {
		t.Error("direct_blocked with nothing sent direct")
	}
}

// waitHealthy waits for a check through the core to pass.
func waitHealthy(t *testing.T, h *harness) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for h.svc.healthOK.Load() == 0 || h.svc.healthFails.Load() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("no check through the core passed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
