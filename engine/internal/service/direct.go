package service

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Some networks let nothing through but a list of allowed addresses (Russian
// mobile operators in "white list" mode), and from abroad, or behind a
// filter, Russian sites may not answer at all. The tunnel works there, but
// whatever the settings send direct does not: the TUN layer reports every
// such connection, refused within milliseconds or timed out after seconds,
// and the sites stay blank. directWatch counts those reports, and once
// direct connections fail by the dozen while the check through the core
// keeps passing, the service says once what goes on and which settings to
// change (an event "direct" and Status.DirectBlocked). It changes nothing
// itself: the app offers the fix and the user decides.

const (
	// directWindow is how far back failures count; directFails of them, to
	// directAddrs different addresses, make the network suspect.
	directWindow = time.Minute
	directFails  = 15
	directAddrs  = 5
	// directProxyShare: the network itself is at fault, not its rules, when
	// connections through the tunnel fail as well: more than one for every
	// directProxyShare direct ones.
	directProxyShare = 5
	// directRepeat is how long the journal stays quiet after the hint, from
	// one connection to the next.
	directRepeat = 30 * time.Minute
)

var (
	// The TUN layer's report of a connection it could not make:
	// "connection: open connection to 1.2.3.4:443 using outbound/direct[direct]: dial tcp 1.2.3.4:443: connect: connection refused".
	directTargetRE = regexp.MustCompile(`open (?:packet )?connection to (\S+) using outbound/`)
	directDialRE   = regexp.MustCompile(`dial (?:tcp|udp)\S* (\S+?):? `)
)

// dialFailure tells whether a line of the TUN layer reports a connection
// that could not be made, and if so where to and whether it was to go
// direct. The causes are those of a network that will not carry it:
// refused, no answer, no route; not a site's own trouble later on.
func dialFailure(line string) (addr string, direct, ok bool) {
	i := strings.Index(line, "outbound/")
	if i < 0 {
		return "", false, false
	}
	l := strings.ToLower(line)
	if !strings.Contains(l, "connection refused") && !strings.Contains(l, "actively refused") &&
		!strings.Contains(l, "i/o timeout") && !strings.Contains(l, "unreachable") && !strings.Contains(l, "no route to host") {
		return "", false, false
	}
	direct = strings.HasPrefix(line[i:], "outbound/direct")
	if m := directTargetRE.FindStringSubmatch(line); m != nil {
		addr = m[1]
	} else if m := directDialRE.FindStringSubmatch(line); m != nil {
		addr = m[1]
	}
	return addr, direct, true
}

type directFail struct {
	at     time.Time
	addr   string
	direct bool
}

// directWatch keeps the count for one connection (gen). Failures count only
// while the check through the core passes; the hint is due at the check
// that passes after the count was reached, so that the tunnel worked all
// along and the network did not merely drop for a moment. A check that
// fails drops the count: then the network or the server is at fault.
type directWatch struct {
	mu     sync.Mutex
	gen    int
	fails  []directFail // within directWindow, oldest first
	armed  int          // direct failures in the window when the count was reached; 0 before
	told   bool         // the hint was given for gen
	toldAt time.Time    // when it was last given, whatever the connection
}

// to starts over for connection gen.
func (w *directWatch) to(gen int) {
	if w.gen != gen {
		w.gen, w.fails, w.armed, w.told = gen, nil, 0, false
	}
}

// fail records a connection of connection gen that could not be made.
func (w *directWatch) fail(gen int, now time.Time, addr string, direct bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.to(gen)
	if w.told {
		return
	}
	w.fails = append(w.fails, directFail{at: now, addr: addr, direct: direct})
	cut := 0
	for cut < len(w.fails) && now.Sub(w.fails[cut].at) > directWindow {
		cut++
	}
	w.fails = w.fails[cut:]
	n, proxied := 0, 0
	addrs := map[string]struct{}{}
	for _, f := range w.fails {
		if !f.direct {
			proxied++
			continue
		}
		n++
		if f.addr != "" {
			addrs[f.addr] = struct{}{}
		}
	}
	switch {
	case proxied*directProxyShare > n:
		// Through the tunnel nothing gets out either.
		w.armed = 0
	case n >= directFails && len(addrs) >= directAddrs:
		w.armed = n
	}
}

// checked takes the result of a check through the core of connection gen.
// It returns, when the hint is due, how many direct connections failed in
// the window, and whether the journal may say so (once in directRepeat).
func (w *directWatch) checked(gen int, now time.Time, passed bool) (n int, due, loud bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.to(gen)
	if !passed {
		w.fails, w.armed = nil, 0
		return 0, false, false
	}
	if w.told || w.armed == 0 {
		return 0, false, false
	}
	n, w.told = w.armed, true
	loud = w.toldAt.IsZero() || now.Sub(w.toldAt) >= directRepeat
	if loud {
		w.toldAt = now
	}
	w.fails, w.armed = nil, 0
	return n, true, loud
}

// noteDial passes a line of the TUN layer to the watch.
func (s *Service) noteDial(line string) {
	addr, direct, ok := dialFailure(line)
	if !ok {
		return
	}
	s.mu.Lock()
	gen, st, o := s.gen, s.status.State, s.connOpts
	s.mu.Unlock()
	// Only while the tunnel is known to work: connected, the last check
	// through the core passed.
	if st != Connected || s.healthFails.Load() > 0 || s.healthOK.Load() == 0 || len(directRoutes(o)) == 0 {
		return
	}
	s.direct.fail(gen, time.Now(), addr, direct)
}

// directChecked takes a check through the active core and gives the hint
// when it is due.
func (s *Service) directChecked(passed bool) {
	s.mu.Lock()
	gen := s.gen
	s.mu.Unlock()
	n, due, loud := s.direct.checked(gen, time.Now(), passed)
	if !due {
		return
	}
	s.mu.Lock()
	if gen != s.gen || s.status.State != Connected {
		s.mu.Unlock()
		return
	}
	s.status.DirectBlocked = true
	routes := directRoutes(s.connOpts)
	s.mu.Unlock()
	e := Event{Kind: "direct", Reason: "blocked"}
	if loud {
		e.Line = directHint(n, routes)
	}
	s.hub.publish(e)
}

// What sends traffic around the tunnel, as directRoutes names it.
const (
	routeRussia   = "russia"   // «Российские сайты напрямую»
	routeSelected = "selected" // the mode «Только выбранное»
	routeLists    = "lists"    // the user's own direct lists
)

// directRoutes lists the settings of o that send traffic direct; none
// without the TUN layer, which alone tells direct from proxied.
func directRoutes(o Options) []string {
	if !o.TUN {
		return nil
	}
	var out []string
	if o.Selective {
		out = append(out, routeSelected)
	}
	// In «Только выбранное» the preset does nothing yet, but it would take
	// over once the mode is «Всё через VPN»: it has to go too.
	if o.DNS.RussiaDirect {
		out = append(out, routeRussia)
	}
	if len(o.DNS.DirectSuffixes) > 0 || len(o.DirectIPs) > 0 || len(o.DirectApps) > 0 {
		out = append(out, routeLists)
	}
	return out
}

// directHint is the journal's line: what was seen, and what to change.
func directHint(n int, routes []string) string {
	var do []string
	has := func(r string) bool { return slices.Contains(routes, r) }
	if has(routeSelected) {
		do = append(do, "включите «Всё через VPN»")
	}
	if has(routeRussia) {
		do = append(do, "выключите «Российские сайты напрямую»")
	}
	advice := ""
	switch {
	case len(do) > 0:
		advice = strings.Join(do, " и ")
		if has(routeLists) {
			advice += "; проверьте и свои списки «напрямую»"
		}
	case has(routeLists):
		advice = "уберите из своих списков «напрямую» то, что здесь не открывается"
	}
	if advice != "" {
		advice = " " + upperFirst(advice) + "."
	}
	return fmt.Sprintf("Прямые соединения не проходят (%d за минуту), а через VPN всё работает: похоже, сеть пропускает только белый список или российские сайты отсюда недоступны.%s", n, advice)
}

// upperFirst capitalises the first letter, whatever its alphabet.
func upperFirst(s string) string {
	for i, r := range s {
		return strings.ToUpper(string(r)) + s[i+len(string(r)):]
	}
	return s
}
