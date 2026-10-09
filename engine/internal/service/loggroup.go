package service

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/proc"
)

// A tunnel that cannot reach its server fails every connection of every app
// the same way: hundreds of lines a minute that differ only in an id, a
// timer and the address, and bury the one line that says what went wrong.
// logGrouper lets the first such error through, counts the repeats and
// reports them as one line every so often. The TUN layer's output and the
// cores' go through it alike.

var (
	// sing-box's own clock, "+0400 2026-10-03 10:26:50 ", as the TUN layer
	// and the sing-box core print it: the journal already shows the time,
	// and it made every line a different one.
	stampRE = regexp.MustCompile(`^[+-]\d{4} \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\s+`)
	// Xray's clock and level: "2026/10/03 12:25:51.212236 [Warning] ".
	xrayStampRE = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)? \[([A-Za-z]+)\] `)
	// sing-box's uptime in seconds after the level, "ERROR[0237] ", which
	// the TUN layer prints where it has no clock (Android, a pipe).
	levelUptimeRE = regexp.MustCompile(`^(TRACE|DEBUG|INFO|WARN|ERROR|FATAL|PANIC) ?\[\d+\] *`)
	// The layer's own bookkeeping, which differs on every line: its uptime
	// "[15370]" and the connection's "[ 534559423 15.12s]"; Xray's session
	// "[1286575286]" is the first kind.
	uptimeRE = regexp.MustCompile(`\[\s*\d+\s*\]`)
	connRE   = regexp.MustCompile(`\[\s*\d+\s+[0-9.]+[a-z0-9.]*\s*\]`)
	// Addresses, alone, with a port or several in brackets: "[a,b,c]";
	// a name with a port, "cp.cloudflare.com:80", Xray's "tcp:" before it.
	listRE     = regexp.MustCompile(`\[[0-9a-fA-F:.,\s]{3,}\]`)
	addrRE     = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b`)
	hostPortRE = regexp.MustCompile(`\b(?:[A-Za-z0-9-]+\.)+[A-Za-z][A-Za-z0-9-]*:\d+\b`)
	spaces     = regexp.MustCompile(`\s+`)
	// The name or address a failure was about: a dead tunnel fails every
	// app's lookups and connections alike, and grouping them by name made
	// a line for each.
	lookupRE       = regexp.MustCompile(`(lookup failed for) [^\s:]+`)
	routerLookupRE = regexp.MustCompile(`(router: lookup) [^\s:]+`)
	targetRE       = regexp.MustCompile(`(open connection to) \S+ (using)`)
	// mihomo names the rule that sent the connection: "(match Match/)",
	// "(match GeoIP/ru)". Every rule's connections fail alike.
	mihomoRuleRE = regexp.MustCompile(`\(match [^)]*\)`)
	// The two lookups of one name, A and AAAA, fail in either order.
	exchangeRE = regexp.MustCompile(`\((exchange6: [^|)]*?) \| (exchange4: [^)]*)\)`)
	// One failed lookup that sing-box reports twice under one request id:
	// "dns: lookup failed for X" and "router: lookup X".
	lookupTwinRE = regexp.MustCompile(`\[\s*(\d+)\s+[0-9.]+[a-z0-9.]*\s*\] (dns: lookup failed for|router: lookup) ([^\s:]+)`)
)

// stripANSI removes the colour codes the TUN layer writes around ERROR.
func stripANSI(s string) string { return proc.StripANSI(s) }

// tidy is the line as the journal shows it: no colour codes, no second
// clock, and the level in one spelling whatever the program, as sing-box
// writes it: "ERROR network: …", "WARN [TCP] dial …", "INFO core: …".
// The app tells errors from warnings by that word.
func tidy(s string) string {
	s = stripANSI(s)
	if s == "" {
		return s
	}
	switch c := s[0]; {
	case c == '+' || c == '-': // sing-box, the TUN layer on the desktop
		s = stampRE.ReplaceAllString(s, "")
	case c == 't': // mihomo
		if t, ok := tidyMihomo(s); ok {
			return t
		}
	case c >= '0' && c <= '9' && len(s) > 20 && s[4] == '/': // Xray
		if m := xrayStampRE.FindStringSubmatchIndex(s); m != nil {
			return levelWord(s[m[2]:m[3]]) + " " + s[m[1]:]
		}
	}
	if s != "" && s[0] >= 'A' && s[0] <= 'Z' && strings.IndexByte(s[:min(len(s), 8)], '[') >= 0 {
		s = levelUptimeRE.ReplaceAllString(s, "$1 ")
	}
	return s
}

// tidyMihomo reads mihomo's
//
//	time="2026-10-04T04:02:05.161704400+03:00" level=warning msg="[TCP] dial …"
//
// as "WARN [TCP] dial …".
func tidyMihomo(s string) (string, bool) {
	if !strings.HasPrefix(s, `time="`) {
		return "", false
	}
	const lv = `" level=`
	i := strings.Index(s, lv)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(lv):]
	j := strings.IndexByte(rest, ' ')
	if j < 0 || !strings.HasPrefix(rest[j+1:], "msg=") {
		return "", false
	}
	level, msg := rest[:j], rest[j+1+len("msg="):]
	if strings.HasPrefix(msg, `"`) {
		if q, err := strconv.QuotedPrefix(msg); err == nil {
			if u, err := strconv.Unquote(q); err == nil {
				msg = u
			}
		}
	}
	return levelWord(level) + " " + msg, true
}

// levelWord spells a level as sing-box does: Xray's "Warning", mihomo's
// "warning" are "WARN".
func levelWord(l string) string {
	l = strings.ToUpper(l)
	if l == "WARNING" {
		return "WARN"
	}
	return l
}

// logShape is the line without what changes from one failure to the next,
// "" for lines that are not errors or warnings, which are never grouped.
func logShape(line string) string {
	if !strings.Contains(line, "ERROR") && !strings.Contains(line, "WARN") {
		return ""
	}
	s := connRE.ReplaceAllString(line, "")
	s = uptimeRE.ReplaceAllString(s, "")
	s = lookupRE.ReplaceAllString(s, "$1 <имя>")
	s = routerLookupRE.ReplaceAllString(s, "$1 <имя>")
	s = targetRE.ReplaceAllString(s, "$1 <адрес> $2")
	s = mihomoRuleRE.ReplaceAllString(s, "(match <правило>)")
	s = exchangeRE.ReplaceAllString(s, "($2 | $1)")
	s = listRE.ReplaceAllString(s, "<адрес>")
	s = addrRE.ReplaceAllString(s, "<адрес>")
	s = hostPortRE.ReplaceAllString(s, "<адрес>")
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// maxLogGroups bounds the memory a flood of ever different errors can take;
// past it lines go through as they are.
const maxLogGroups = 256

// maxLookupTwins is how many recent failed lookups are kept to recognise
// the second report of one: sing-box writes the two together.
const maxLookupTwins = 64

type logGroup struct {
	shape   string
	source  string
	repeats int       // not reported yet
	since   time.Time // the start of the period the repeats are counted in
	last    time.Time // the latest repeat
	timer   *time.Timer
}

type logGrouper struct {
	every time.Duration // how often a repeating line is reported
	// emit passes a line on; at is when it was seen, zero for now.
	emit   func(source, line string, at time.Time)
	now    func() time.Time
	mu     sync.Mutex
	groups map[string]*logGroup
	// Failed lookups lately seen, by request id and name: which report
	// came ("dns: lookup failed for", "router: lookup", "both"), and the
	// order they came in, to forget the oldest.
	twins     map[string]string
	twinOrder []string
}

func newLogGrouper(every time.Duration, emit func(source, line string, at time.Time)) *logGrouper {
	return &logGrouper{every: every, emit: emit, now: time.Now, groups: map[string]*logGroup{}, twins: map[string]string{}}
}

// add passes a line on, or counts it as a repeat of one passed on lately.
func (g *logGrouper) add(source, line string) {
	if line = tidy(line); !g.twin(line) {
		g.addTidy(source, line)
	}
}

// addTidy is add for a line already tidied and not a twin.
func (g *logGrouper) addTidy(source, line string) {
	shape := logShape(line)
	if shape == "" {
		g.emit(source, line, time.Time{})
		return
	}
	g.addShape(source, line, shape)
}

// twin reports whether a tidied line is the second report of a failed
// lookup: sing-box writes "dns: lookup failed for X" and then
// "router: lookup X" with the same request id and the same cause.
func (g *logGrouper) twin(line string) bool {
	if !strings.Contains(line, "lookup") {
		return false
	}
	m := lookupTwinRE.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	key, kind := m[1]+"\x00"+m[3], m[2]
	g.mu.Lock()
	defer g.mu.Unlock()
	if seen, ok := g.twins[key]; ok {
		if seen == kind {
			return false
		}
		g.twins[key] = "both"
		return true
	}
	if len(g.twinOrder) >= maxLookupTwins {
		delete(g.twins, g.twinOrder[0])
		g.twinOrder = append(g.twinOrder[:0], g.twinOrder[1:]...)
	}
	g.twins[key] = kind
	g.twinOrder = append(g.twinOrder, key)
	return false
}

// addAs passes text on, or counts it as a repeat: for lines that say the
// same in other words, whatever their own text.
func (g *logGrouper) addAs(source, text string) { g.addShape(source, text, text) }

func (g *logGrouper) addShape(source, line, shape string) {
	key := source + "\x00" + shape
	g.mu.Lock()
	if grp, ok := g.groups[key]; ok {
		grp.repeats++
		grp.last = g.now()
		g.mu.Unlock()
		return
	}
	if len(g.groups) >= maxLogGroups {
		g.mu.Unlock()
		g.emit(source, line, time.Time{})
		return
	}
	grp := &logGroup{shape: shape, source: source, since: g.now()}
	g.groups[key] = grp
	grp.timer = time.AfterFunc(g.every, func() { g.flush(key, grp) })
	g.mu.Unlock()
	g.emit(source, line, time.Time{})
}

// flush reports the repeats since the last report. A period without any
// ends the group: the next such line is a new one and shown in full. A
// timer that fired as flushAll ended its group finds another or none.
func (g *logGrouper) flush(key string, grp *logGroup) {
	g.mu.Lock()
	if g.groups[key] != grp {
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
	grp.since = g.now()
	grp.timer.Reset(g.every)
	g.mu.Unlock()
	g.emit(grp.source, repeatsText(grp.shape, n, g.every), time.Time{})
}

// flushAll reports the repeats not reported yet and ends every group: the
// connection that made them is over. The counts come before the journal's
// "отключено", each at the time of its last repeat and for the time it
// took, not at the end of a period that would close after the disconnect.
func (g *logGrouper) flushAll() {
	type pending struct {
		source, text string
		at           time.Time
	}
	if g == nil {
		return
	}
	var out []pending
	g.mu.Lock()
	for key, grp := range g.groups {
		grp.timer.Stop()
		delete(g.groups, key)
		if grp.repeats > 0 {
			out = append(out, pending{grp.source, repeatsText(grp.shape, grp.repeats, grp.last.Sub(grp.since)), grp.last})
		}
	}
	g.mu.Unlock()
	slices.SortFunc(out, func(a, b pending) int { return a.at.Compare(b.at) })
	for _, p := range out {
		g.emit(p.source, p.text, p.at)
	}
}

// repeatsText is a group's count: "<line> — ещё 2 раза за 30 с". Less
// than a second is told as one.
func repeatsText(shape string, n int, period time.Duration) string {
	period = max(period.Round(time.Second), time.Second)
	return fmt.Sprintf("%s — ещё %d %s за %s", shape, n, ruPlural(n, "раз", "раза", "раз"), durationText(period))
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
