package service

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Lines as the TUN layer wrote them while no core could reach the server:
// colour codes, its uptime, the connection's id and age, and the target.
const (
	floodA = "\x1b[31mERROR\x1b[0m[15401] [ \x1b[38;5;169m3007246489\x1b[0m 15.12s] connection: open connection to 194.221.250.50:443 using outbound/socks[proxy]: socks5: request rejected, code=1"
	floodB = "\x1b[31mERROR\x1b[0m[15408] [ \x1b[38;5;120m2743022447\x1b[0m 15.32s] connection: open connection to [152.32.228.184] using outbound/socks[proxy]: socks5: request rejected, code=1"
	floodC = "\x1b[31mERROR\x1b[0m[15421] [ \x1b[38;5;49m1838926625\x1b[0m 2m12s] connection: open connection to [23.207.210.130,23.207.210.157,23.207.210.158] using outbound/socks[proxy]: socks5: request rejected, code=1"
	other  = "\x1b[31mERROR\x1b[0m[15500] [ \x1b[38;5;49m1111\x1b[0m 0ms] connection: open connection to 194.221.250.50:443 using outbound/socks[proxy]: dial tcp 127.0.0.1:17890: connect: connection refused"
)

type collected struct {
	mu    sync.Mutex
	lines []string
}

func (c *collected) emit(source, line string) {
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
	if got := c.get(); len(got) != 1 || strings.Contains(got[0], "\x1b") || !strings.Contains(got[0], "194.221.250.50:443") {
		t.Fatalf("right away: %q, want the first error alone, without colour codes", got)
	}
	time.Sleep(120 * time.Millisecond)
	got := c.get()
	if len(got) != 2 || !strings.Contains(got[1], "ещё 3 раз") || !strings.Contains(got[1], "<адрес>") {
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
	if len(got) != 2 || !strings.Contains(got[1], "152.32.228.184") {
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
	g.add("tun", "+0400 2026-10-03 10:26:50 ERROR [1365276019 5.4s] connection: open connection to 77.223.124.134:443 using outbound/direct[direct]: dial tcp 77.223.124.134:443: i/o timeout")
	g.add("tun", "+0400 2026-10-03 10:26:55 ERROR [3883584643 5.0s] connection: open connection to 77.223.124.135:443 using outbound/direct[direct]: dial tcp 77.223.124.135:443: i/o timeout")
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
	a := logShape("ERROR [1010360205 5.0s] connection: open connection to cp.cloudflare.com:80 using outbound/vless[proxy]: dial tcp 179.254.115.13:443: i/o timeout")
	b := logShape("ERROR [4052028970 5.0s] connection: open connection to captive.apple.com:80 using outbound/vless[proxy]: dial tcp 179.254.115.13:443: i/o timeout")
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
