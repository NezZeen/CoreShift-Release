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
