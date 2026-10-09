package service

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/supervisor"
)

// Lines as the TUN layer wrote them while no core could reach the server:
// colour codes, its uptime, the connection's id and age, and the target.
const (
	floodA = "\x1b[31mERROR\x1b[0m[15401] [ \x1b[38;5;169m3007246489\x1b[0m 15.12s] connection: open connection to 203.0.113.50:443 using outbound/socks[proxy]: socks5: request rejected, code=1"
	floodB = "\x1b[31mERROR\x1b[0m[15408] [ \x1b[38;5;120m2743022447\x1b[0m 15.32s] connection: open connection to [203.0.113.184] using outbound/socks[proxy]: socks5: request rejected, code=1"
	floodC = "\x1b[31mERROR\x1b[0m[15421] [ \x1b[38;5;49m1838926625\x1b[0m 2m12s] connection: open connection to [23.207.210.130,23.207.210.157,23.207.210.158] using outbound/socks[proxy]: socks5: request rejected, code=1"
	other  = "\x1b[31mERROR\x1b[0m[15500] [ \x1b[38;5;49m1111\x1b[0m 0ms] connection: open connection to 203.0.113.50:443 using outbound/socks[proxy]: dial tcp 127.0.0.1:17890: connect: connection refused"
)

type collected struct {
	mu    sync.Mutex
	lines []string
}

func (c *collected) emit(source, line string, _ time.Time) {
	c.mu.Lock()
	c.lines = append(c.lines, source+": "+line)
	c.mu.Unlock()
}

func (c *collected) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

func TestLogShapeIgnoresWhatChanges(t *testing.T) {
	a, b, c := logShape(stripANSI(floodA)), logShape(stripANSI(floodB)), logShape(stripANSI(floodC))
	if a == "" || a != b || a != c {
		t.Errorf("one failure, three shapes:\n%q\n%q\n%q", a, b, c)
	}
	if want := "ERROR connection: open connection to <адрес> using outbound/socks[proxy]: socks5: request rejected, code=1"; a != want {
		t.Errorf("shape = %q, want %q", a, want)
	}
	// Another cause is another line, whatever the address.
	if logShape(stripANSI(other)) == a {
		t.Error("a refused connection is grouped with a rejected request")
	}
}

func TestLogShapeLeavesOrdinaryLinesAlone(t *testing.T) {
	for _, l := range []string{
		"INFO [12] inbound/tun[tun-in]: started",
		"outbound/direct[direct]: outbound connection to 1.1.1.1:443",
		"",
	} {
		if s := logShape(l); s != "" {
			t.Errorf("%q has the shape %q: only errors and warnings are grouped", l, s)
		}
	}
}

func TestStripANSI(t *testing.T) {
	// The escape byte is lost when logs are pasted: both forms go.
	for in, want := range map[string]string{
		"\x1b[31mERROR\x1b[0m[1] x":      "ERROR[1] x",
		"[31mERROR [0m[1] x":             "ERROR [1] x",
		"[38;5;207m534559423 [0m 0ms] x": "534559423  0ms] x",
		"keep [a,b] and [3]":             "keep [a,b] and [3]",
	} {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGrouperShowsTheFirstAndCountsTheRest(t *testing.T) {
	var c collected
	g := newLogGrouper(40*time.Millisecond, c.emit)
	g.add("tun", floodA)
	g.add("tun", floodB)
	g.add("tun", floodC)
	g.add("tun", floodA)
	if got := c.get(); len(got) != 1 || strings.Contains(got[0], "\x1b") || !strings.Contains(got[0], "203.0.113.50:443") {
		t.Fatalf("right away: %q, want the first error alone, without colour codes", got)
	}
	time.Sleep(120 * time.Millisecond)
	got := c.get()
	if len(got) != 2 || !strings.Contains(got[1], "ещё 3 раза за") || !strings.Contains(got[1], "<адрес>") {
		t.Fatalf("after a period: %q, want one summary of 3 repeats", got)
	}
}

func TestGrouperForgetsAQuietGroup(t *testing.T) {
	var c collected
	g := newLogGrouper(30*time.Millisecond, c.emit)
	g.add("tun", floodA)
	time.Sleep(120 * time.Millisecond) // a period with nothing, then the group ends
	g.add("tun", floodB)
	got := c.get()
	if len(got) != 2 || !strings.Contains(got[1], "203.0.113.184") {
		t.Errorf("%q: after a quiet period the error is shown in full again", got)
	}
}

func TestGrouperKeepsSourcesAndCausesApart(t *testing.T) {
	var c collected
	g := newLogGrouper(time.Minute, c.emit)
	g.add("tun", floodA)
	g.add("tun", other)
	g.add("xray", floodA)
	g.add("tun", "INFO ordinary line")
	g.add("tun", "INFO ordinary line")
	if got := c.get(); len(got) != 5 {
		t.Errorf("%d lines out, want all 5 (nothing here repeats an error): %q", len(got), got)
	}
}

func TestGrouperBoundsItsMemory(t *testing.T) {
	var c collected
	g := newLogGrouper(time.Minute, c.emit)
	for i := range maxLogGroups + 50 {
		g.add("tun", "ERROR unique failure "+strings.Repeat("x", i+1))
	}
	if got := c.get(); len(got) != maxLogGroups+50 {
		t.Errorf("%d lines out, want every one: past the limit they are not grouped", len(got))
	}
	g.mu.Lock()
	n := len(g.groups)
	g.mu.Unlock()
	if n > maxLogGroups {
		t.Errorf("%d groups kept, limit %d", n, maxLogGroups)
	}
}

func TestDurationText(t *testing.T) {
	for d, want := range map[time.Duration]string{30 * time.Second: "30 с", 2 * time.Minute: "2 мин", 90 * time.Second: "90 с"} {
		if got := durationText(d); got != want {
			t.Errorf("durationText(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestGrouperDropsTheLayersOwnClock(t *testing.T) {
	var c collected
	g := newLogGrouper(40*time.Millisecond, c.emit)
	g.add("tun", "+0400 2026-10-03 10:26:50 ERROR [1365276019 5.4s] connection: open connection to 198.51.100.134:443 using outbound/direct[direct]: dial tcp 198.51.100.134:443: i/o timeout")
	g.add("tun", "+0400 2026-10-03 10:26:55 ERROR [3883584643 5.0s] connection: open connection to 198.51.100.135:443 using outbound/direct[direct]: dial tcp 198.51.100.135:443: i/o timeout")
	got := c.get()
	if len(got) != 1 || !strings.HasPrefix(got[0], "tun: ERROR ") {
		t.Fatalf("right away: %q, want one line without the layer's clock", got)
	}
	time.Sleep(120 * time.Millisecond)
	got = c.get()
	if len(got) != 2 || !strings.HasPrefix(got[1], "tun: ERROR connection: open connection to <адрес>") || !strings.Contains(got[1], "ещё 1 раз") {
		t.Fatalf("after a period: %q, want the two timeouts as one group", got)
	}
}

// Lines from the Android journal of 2026-10-03: the server did not answer,
// and every app's lookups through the tunnel timed out, each name a line.
var deadLookups = []string{
	"ERROR[2770] [3310814267 10.0s] dns: lookup failed for search32-normal-useast1a.tiktokv.com: (exchange4: context deadline exceeded | exchange6: context deadline exceeded)",
	"ERROR[2770] [859274356 20.2s] dns: lookup failed for log22-normal-alisg.tiktokv.com: (exchange6: context deadline exceeded | exchange4: context deadline exceeded)",
	"ERROR[2780] [4040236007 1m0s] dns: lookup failed for rezvorck.github.io: (exchange4: context deadline exceeded | exchange6: context deadline exceeded)",
	"ERROR[2780] [4288490590 20.3s] dns: lookup failed for ru-comort-stsdk.vivoglobal.com: (exchange4: context deadline exceeded | exchange6: context deadline exceeded)",
}

func TestLookupFailuresGroupByCauseNotName(t *testing.T) {
	want := logShape(deadLookups[0])
	if want != "ERROR dns: lookup failed for <имя>: (exchange4: context deadline exceeded | exchange6: context deadline exceeded)" {
		t.Errorf("shape = %q", want)
	}
	for _, l := range deadLookups[1:] {
		if got := logShape(l); got != want {
			t.Errorf("%q: shape %q, want %q", l, got, want)
		}
	}
	// One lookup of the two is another cause.
	if logShape("ERROR[2784] [4060424138 10.0s] dns: lookup failed for ru-comonrt-stsdk.vivoglobal.com: exchange4: context deadline exceeded") == want {
		t.Error("a single failed lookup grouped with a double one")
	}
	// Connections by name too.
	a := logShape("ERROR [1010360205 5.0s] connection: open connection to cp.cloudflare.com:80 using outbound/vless[proxy]: dial tcp 203.0.113.13:443: i/o timeout")
	b := logShape("ERROR [4052028970 5.0s] connection: open connection to captive.apple.com:80 using outbound/vless[proxy]: dial tcp 203.0.113.13:443: i/o timeout")
	if a != b || strings.Contains(a, "cloudflare") {
		t.Errorf("connections by name: %q and %q", a, b)
	}
}

// While the server does not answer, the flood of timed-out lookups is one
// line that says why, counted like any other repeat.
func TestDeadUpstreamLookupsAreOneLine(t *testing.T) {
	var c collected
	s := &Service{logs: newLogGrouper(40*time.Millisecond, c.emit)}
	s.Log("tun", deadLookups[0])
	if got := c.get(); len(got) != 1 || !strings.Contains(got[0], "tiktokv.com") {
		t.Fatalf("before the checks fail: %q, want the line itself", got)
	}
	s.healthFails.Store(upstreamDeadChecks)
	for _, l := range append(deadLookups, deadLookups...) {
		s.Log("tun", l)
	}
	s.Log("tun", "ERROR[2790] some other failure")
	got := c.get()
	if len(got) != 3 || got[1] != "tun: "+upstreamDNSDead || !strings.Contains(got[2], "some other failure") {
		t.Fatalf("while the server is down: %q", got)
	}
	time.Sleep(120 * time.Millisecond)
	got = c.get()
	if len(got) < 4 || !strings.HasPrefix(got[3], "tun: "+upstreamDNSDead+" — ещё 7 раз") {
		t.Fatalf("after a period: %q", got)
	}
}

// Lines from a Windows journal of 0.7.0, 2026-10-04: the network was gone
// and every core printed every failed connection, the health checks' among
// them, in its own format. Xray's is its format with the same failure.
const (
	singboxNoIface = "+0300 2026-10-04 04:01:55 ERROR network: missing default interface"
	singboxDial    = "+0300 2026-10-04 04:01:55 ERROR [902621915 4ms] connection: open connection to cp.cloudflare.com:80 using outbound/vless[proxy]: dial tcp 203.0.113.181:443: connectex: A socket operation was attempted to an unreachable host."
	mihomoDial     = `time="2026-10-04T04:02:05.161704400+03:00" level=warning msg="[TCP] dial proxy (match Match/) 127.0.0.1:51790 --> cp.cloudflare.com:80 error: 203.0.113.181:443 connect error: dial tcp 203.0.113.181:443: connectex: A socket operation was attempted to an unreachable host."`
	xrayDial       = "2026/10/04 04:02:05.161704 [Warning] [1286575286] app/proxyman/inbound: connection ends > proxy/vless/outbound: failed to find an available destination > common/retry: all retry attempts failed > transport/internet/tcp: failed to dial tcp:cp.cloudflare.com:80 via 203.0.113.181:443 > dial tcp 203.0.113.181:443: connectex: A socket operation was attempted to an unreachable host."
)

var coreDials = map[core.Kind]string{core.SingBox: singboxDial, core.Mihomo: mihomoDial, core.Xray: xrayDial}

// The same failure of each core a moment later, for another app or check:
// another id, port, age, rule and target.
func againLater(line string) string {
	return strings.NewReplacer(
		"902621915 4ms", "1406313530 2.31s",
		"1286575286", "3913461297",
		"127.0.0.1:51790", "127.0.0.1:51811",
		"cp.cloudflare.com:80", "www.gstatic.com:80",
		"(match Match/)", "(match GeoIP/telegram)",
		"04:01:55", "04:02:09", "04:02:05.161704", "04:02:09.004417",
	).Replace(line)
}

func TestTidyReadsEachCoresFormat(t *testing.T) {
	for in, want := range map[string]string{
		singboxNoIface: "ERROR network: missing default interface",
		singboxDial:    "ERROR [902621915 4ms] connection: open connection to cp.cloudflare.com:80 using outbound/vless[proxy]: dial tcp 203.0.113.181:443: connectex: A socket operation was attempted to an unreachable host.",
		mihomoDial:     "WARN [TCP] dial proxy (match Match/) 127.0.0.1:51790 --> cp.cloudflare.com:80 error: 203.0.113.181:443 connect error: dial tcp 203.0.113.181:443: connectex: A socket operation was attempted to an unreachable host.",
		xrayDial:       "WARN [1286575286] app/proxyman/inbound: connection ends > proxy/vless/outbound: failed to find an available destination > common/retry: all retry attempts failed > transport/internet/tcp: failed to dial tcp:cp.cloudflare.com:80 via 203.0.113.181:443 > dial tcp 203.0.113.181:443: connectex: A socket operation was attempted to an unreachable host.",
		`time="2026-10-04T04:00:01.5+03:00" level=info msg="Start initial configuration in progress"`: "INFO Start initial configuration in progress",
		`time="2026-10-04T04:00:01.5+03:00" level=error msg="say \"hi\""`:                             `ERROR say "hi"`,
		"2026/10/03 12:25:51.212236 [Warning] core: Xray 26.3.27 started":                             "WARN core: Xray 26.3.27 started",
		"2026/10/03 12:25:51.210168 [Info] infra/conf/serial: Reading config: x":                      "INFO infra/conf/serial: Reading config: x",
		// The TUN layer on Android: no clock, its uptime after the level.
		"ERROR[0237] [3308650319 10.0s] dns: lookup failed for a.example: x": "ERROR [3308650319 10.0s] dns: lookup failed for a.example: x",
		"FATAL[0015] start service: x":                                       "FATAL start service: x",
		"WARN [0012] x":                                                      "WARN x",
		// What is in no known format stays as it is.
		"Xray 26.3.27 (Xray, Penetrates Everything.) d2758a0": "Xray 26.3.27 (Xray, Penetrates Everything.) d2758a0",
		`time="broken`:           `time="broken`,
		"[TCP] 127.0.0.1:1 -> x": "[TCP] 127.0.0.1:1 -> x",
		"":                       "",
	} {
		if got := tidy(in); got != want {
			t.Errorf("tidy(%q)\n = %q\nwant %q", in, got, want)
		}
	}
}

func TestEachCoresFloodIsOneShape(t *testing.T) {
	for k, line := range coreDials {
		a, b := logShape(tidy(line)), logShape(tidy(againLater(line)))
		if a == "" || a != b {
			t.Errorf("%s: one failure, two shapes:\n%q\n%q", k, a, b)
		}
		for _, gone := range []string{"51790", "902621915", "1286575286", "cloudflare", "203.0.113.181", "Match/"} {
			if strings.Contains(a, gone) {
				t.Errorf("%s: %q is in the shape %q", k, gone, a)
			}
		}
	}
	if logShape(tidy(singboxNoIface)) == logShape(tidy(singboxDial)) {
		t.Error("another cause is another line")
	}
}

// The health checks go through the core every few seconds and fail as
// every app does: the first is shown, the rest are a count every period.
func TestCoreOutputIsGroupedForEveryCore(t *testing.T) {
	var c collected
	s := &Service{logs: newLogGrouper(60*time.Millisecond, c.emit)}
	for k, line := range coreDials {
		for i := range 40 {
			l := line
			if i%2 == 1 {
				l = againLater(line)
			}
			s.onCoreEvent(supervisor.Event{Kind: supervisor.EventLog, Core: k, Line: l})
		}
	}
	s.onCoreEvent(supervisor.Event{Kind: supervisor.EventLog, Core: core.SingBox, Line: singboxNoIface})
	got := c.get()
	if len(got) != 4 {
		t.Fatalf("right away %d lines, want the first of each core and the other cause:\n%s", len(got), strings.Join(got, "\n"))
	}
	for _, want := range []string{
		"sing-box: ERROR [902621915 4ms] connection: open connection to cp.cloudflare.com:80",
		"mihomo: WARN [TCP] dial proxy (match Match/) 127.0.0.1:51790 --> cp.cloudflare.com:80",
		"xray: WARN [1286575286] app/proxyman/inbound",
		"sing-box: ERROR network: missing default interface",
	} {
		if !slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, want) }) {
			t.Errorf("no line %q in\n%s", want, strings.Join(got, "\n"))
		}
	}
	time.Sleep(200 * time.Millisecond)
	got = c.get()
	sums := 0
	for _, l := range got[4:] {
		if strings.Contains(l, "— ещё 39 раз за") {
			sums++
		}
	}
	if sums != 3 || len(got) != 7 {
		t.Fatalf("after a period, want one count of 39 for each core:\n%s", strings.Join(got, "\n"))
	}
}

// On Android the TUN layer prints its uptime after the level, and every
// failed lookup twice under one request id: "dns: lookup failed for X"
// and "router: lookup X" (the user's journal of 0.7.0).
func TestAndroidLookupIsOneLine(t *testing.T) {
	const (
		dnsLine    = "ERROR[0237] [3308650319 10.0s] dns: lookup failed for ru-comort-stsdk.vivoglobal.com: (exchange4: context deadline exceeded | exchange6: context deadline exceeded)"
		routerLine = "ERROR[0237] [3308650319 10.0s] router: lookup ru-comort-stsdk.vivoglobal.com: (exchange4: context deadline exceeded | exchange6: context deadline exceeded)"
		otherID    = "ERROR[0238] [12345 1.0s] router: lookup ru-comort-stsdk.vivoglobal.com: (exchange4: context deadline exceeded | exchange6: context deadline exceeded)"
	)
	var c collected
	s := &Service{logs: newLogGrouper(time.Minute, c.emit)}
	s.Log("tun", dnsLine)
	s.Log("tun", routerLine)
	got := c.get()
	if len(got) != 1 || got[0] != "tun: ERROR [3308650319 10.0s] dns: lookup failed for ru-comort-stsdk.vivoglobal.com: (exchange4: context deadline exceeded | exchange6: context deadline exceeded)" {
		t.Fatalf("one lookup, one line without the uptime: %q", got)
	}
	// Another request is another failure, shown, not dropped.
	s.Log("tun", otherID)
	if got := c.get(); len(got) != 2 || !strings.Contains(got[1], "router: lookup") {
		t.Fatalf("another request: %q", got)
	}
	// Either order.
	var c2 collected
	s2 := &Service{logs: newLogGrouper(time.Minute, c2.emit)}
	s2.Log("tun", routerLine)
	s2.Log("tun", dnsLine)
	if got := c2.get(); len(got) != 1 || !strings.Contains(got[0], "router: lookup") {
		t.Fatalf("router first: %q", got)
	}
	// While the server is down both are the one line that says so.
	var c3 collected
	s3 := &Service{logs: newLogGrouper(time.Minute, c3.emit)}
	s3.healthFails.Store(upstreamDeadChecks)
	s3.Log("tun", dnsLine)
	s3.Log("tun", routerLine)
	s3.Log("tun", otherID)
	s3.logs.mu.Lock()
	n := s3.logs.groups["tun\x00"+upstreamDNSDead].repeats
	s3.logs.mu.Unlock()
	if got := c3.get(); len(got) != 1 || got[0] != "tun: "+upstreamDNSDead || n != 1 {
		t.Fatalf("server down: %q, %d repeats (want 1: the other request)", got, n)
	}
}

// A disconnect tells the repeats counted so far at once, at the time of
// the last one and for the time they took (the journal of 0.8.1 had them
// 3–20 s after "отключено"), and ends the groups.
func TestFlushAllReportsPendingRepeats(t *testing.T) {
	type line struct {
		text string
		at   time.Time
	}
	var mu sync.Mutex
	var got []line
	g := newLogGrouper(200*time.Millisecond, func(source, l string, at time.Time) {
		mu.Lock()
		got = append(got, line{source + ": " + l, at})
		mu.Unlock()
	})
	t0 := time.Date(2026, 10, 9, 10, 7, 15, 0, time.Local)
	clock := t0
	g.now = func() time.Time { return clock }
	g.add("tun", floodA)
	clock = t0.Add(2 * time.Second)
	g.add("tun", floodB)
	clock = t0.Add(5 * time.Second)
	g.add("tun", floodC)
	g.add("sing-box", singboxNoIface) // shown, never repeated: nothing to tell
	g.flushAll()
	mu.Lock()
	n := len(got)
	sum := got[n-1]
	mu.Unlock()
	if n != 3 || sum.text != "tun: ERROR connection: open connection to <адрес> using outbound/socks[proxy]: socks5: request rejected, code=1 — ещё 2 раза за 5 с" {
		t.Fatalf("after the flush: %v", got)
	}
	if !sum.at.Equal(t0.Add(5 * time.Second)) {
		t.Errorf("the count bears %v, want the last repeat's %v", sum.at, t0.Add(5*time.Second))
	}
	g.mu.Lock()
	left := len(g.groups)
	g.mu.Unlock()
	if left != 0 {
		t.Errorf("%d groups left after the flush", left)
	}
	// The periods' timers are stopped: nothing more comes, and the next
	// such line is a new connection's, shown in full.
	time.Sleep(400 * time.Millisecond)
	g.add("tun", floodB)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 4 || !strings.Contains(got[3].text, "203.0.113.184") || !got[3].at.IsZero() {
		t.Errorf("after the flush: %v", got)
	}
}

// Disconnecting puts the counts of the connection before "отключено".
func TestDisconnectFlushesGroupsBeforeIdle(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.svc.Log("tun", floodA)
	h.svc.Log("tun", floodB)
	h.svc.Log("tun", floodC)
	h.svc.Disconnect()
	var sum *Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-h.events:
			if e.Kind == "log" && strings.Contains(e.Line, "— ещё 2 раза за") {
				sum = &e
			}
			if e.Kind == "state" && e.State == Idle {
				if sum == nil {
					t.Fatal("the count did not come before the idle state")
				}
				if sum.Time.After(e.Time) {
					t.Errorf("the count at %v is after the idle state at %v", sum.Time, e.Time)
				}
				return
			}
		case <-deadline:
			t.Fatal("no idle state")
		}
	}
}

// Remarks the cores make on every start, harmless and the same every time,
// are not in the journal; another warning of the same kind is.
func TestStartupRemarksAreDropped(t *testing.T) {
	var c collected
	s := &Service{logs: newLogGrouper(time.Minute, c.emit)}
	for source, l := range map[string]string{
		"sing-box": "+0300 2026-10-09 10:07:01 WARN network: initialize package manager: read packages list: open /data/system/packages.xml: permission denied",
		"mihomo":   `time="2026-10-09T10:07:01.5+03:00" level=info msg="Geodata Loader mode: memconservative"`,
		"mihomo ":  `time="2026-10-09T10:07:01.5+03:00" level=info msg="Geosite Matcher implementation: succinct"`,
	} {
		s.Log(source, l)
		s.Log(source, l)
	}
	if got := c.get(); len(got) != 0 {
		t.Fatalf("dropped lines got through: %q", got)
	}
	s.Log("sing-box", "WARN network: initialize package manager: create package manager: something new")
	s.Log("mihomo", `time="2026-10-09T10:07:01.5+03:00" level=warning msg="Geodata Loader mode: broken"`)
	if got := c.get(); len(got) != 2 {
		t.Errorf("other lines must stay: %q", got)
	}
}

func TestLookupTwinsAreBounded(t *testing.T) {
	g := newLogGrouper(time.Minute, func(string, string, time.Time) {})
	for i := range 3 * maxLookupTwins {
		g.twin(fmt.Sprintf("ERROR [%d 1.0s] dns: lookup failed for a%d.example: x", i, i))
	}
	if len(g.twins) > maxLookupTwins || len(g.twinOrder) > maxLookupTwins {
		t.Errorf("%d twins kept, limit %d", len(g.twins), maxLookupTwins)
	}
}

// Every line the grouper takes goes through tidy and, for errors, logShape:
// both have to stay cheap next to a flood.
func BenchmarkLogFlood(b *testing.B) {
	g := newLogGrouper(time.Minute, func(string, string, time.Time) {})
	lines := []string{singboxDial, mihomoDial, xrayDial, againLater(mihomoDial), "INFO ordinary line"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		g.add("core", lines[i%len(lines)])
	}
}
