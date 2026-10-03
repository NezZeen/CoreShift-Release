package service

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/proc"
)

// A tunnel that cannot reach its server fails every connection of every app
// the same way: hundreds of lines a minute that differ only in an id, a
// timer and the address, and bury the one line that says what went wrong.
// logGrouper lets the first such error through, counts the repeats and
// reports them as one line every so often.

var (
	// The TUN layer's own clock, "+0400 2026-10-03 10:26:50 ", which the
	// journal already shows and which made every line a different one.
	stampRE = regexp.MustCompile(`^[+-]\d{4} \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\s+`)
	// The layer's own bookkeeping, which differs on every line: its uptime
	// "[15370]" and the connection's "[ 534559423 15.12s]".
	uptimeRE = regexp.MustCompile(`\[\s*\d+\s*\]`)
	connRE   = regexp.MustCompile(`\[\s*\d+\s+[0-9.]+[a-z0-9.]*\s*\]`)
	// Addresses, alone, with a port or several in brackets: "[a,b,c]".
	listRE = regexp.MustCompile(`\[[0-9a-fA-F:.,\s]{3,}\]`)
	addrRE = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b`)
	spaces = regexp.MustCompile(`\s+`)
)

// stripANSI removes the colour codes the TUN layer writes around ERROR.
func stripANSI(s string) string { return proc.StripANSI(s) }

// tidy is the line as the journal shows it: no colour codes, no second clock.
func tidy(s string) string { return stampRE.ReplaceAllString(stripANSI(s), "") }

// logShape is the line without what changes from one failure to the next,
// "" for lines that are not errors or warnings, which are never grouped.
func logShape(line string) string {
	if !strings.Contains(line, "ERROR") && !strings.Contains(line, "WARN") {
		return ""
	}
	s := connRE.ReplaceAllString(line, "")
	s = uptimeRE.ReplaceAllString(s, "")
	s = listRE.ReplaceAllString(s, "<адрес>")
	s = addrRE.ReplaceAllString(s, "<адрес>")
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// maxLogGroups bounds the memory a flood of ever different errors can take;
// past it lines go through as they are.
const maxLogGroups = 256

type logGroup struct {
	shape   string
	source  string
	repeats int // not reported yet
	timer   *time.Timer
}

type logGrouper struct {
	every  time.Duration // how often a repeating line is reported
	emit   func(source, line string)
	mu     sync.Mutex
	groups map[string]*logGroup
}

func newLogGrouper(every time.Duration, emit func(source, line string)) *logGrouper {
	return &logGrouper{every: every, emit: emit, groups: map[string]*logGroup{}}
}

// add passes a line on, or counts it as a repeat of one passed on lately.
func (g *logGrouper) add(source, line string) {
	line = tidy(line)
	shape := logShape(line)
	if shape == "" {
		g.emit(source, line)
		return
	}
	key := source + "\x00" + shape
	g.mu.Lock()
	if grp, ok := g.groups[key]; ok {
		grp.repeats++
		g.mu.Unlock()
		return
	}
	if len(g.groups) >= maxLogGroups {
		g.mu.Unlock()
		g.emit(source, line)
		return
	}
	grp := &logGroup{shape: shape, source: source}
	g.groups[key] = grp
	grp.timer = time.AfterFunc(g.every, func() { g.flush(key) })
	g.mu.Unlock()
	g.emit(source, line)
}

// flush reports the repeats since the last report. A period without any
// ends the group: the next such line is a new one and shown in full.
func (g *logGrouper) flush(key string) {
	g.mu.Lock()
	grp, ok := g.groups[key]
	if !ok {
		g.mu.Unlock()
		return
	}
	n := grp.repeats
	if n == 0 {
		delete(g.groups, key)
		g.mu.Unlock()
		return
	}
	grp.repeats = 0
	grp.timer.Reset(g.every)
	g.mu.Unlock()
	g.emit(grp.source, fmt.Sprintf("%s — ещё %d раз за %s", grp.shape, n, durationText(g.every)))
}

// durationText is a period for the log: "30 с", "2 мин".
func durationText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d мин", int(d/time.Minute))
	}
	if d >= time.Second {
		return fmt.Sprintf("%d с", int(d.Round(time.Second)/time.Second))
	}
	return d.String()
}
