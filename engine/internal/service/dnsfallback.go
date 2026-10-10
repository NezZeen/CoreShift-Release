package service

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/dnsprobe"
	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/tunlayer"
)

// The user may set their own resolver for the sites that go around the VPN
// (dns.direct): an ad-blocking one like AdGuard's, by address. On a network
// that blocks it (a mobile operator's white list, a filter on DNS over HTTPS)
// every name that goes direct would stay unresolved and the sites would not
// open, so the TUN layer asks the network's own resolvers for what the
// user's does not answer (tunlayer.DNSOptions.DirectFallback): that is what
// keeps the names resolving, lookup by lookup, with no help from here.
//
// What this file adds is the word. The layer's journal says every failed
// lookup of the user's resolver; once they pile up, the resolver is tried
// by a lookup of our own (the same as the settings page's check, dnscheck.go)
// and, if it really does not answer, the journal and the home page say so,
// once, with the reason. The note is taken back when the resolver answers
// again, which is checked every few minutes meanwhile. Nothing here changes
// the connection: it only tells.
//
// The resolver the tunnel carries (dns.remote) has no such fallback. It is
// reached through the tunnel, where a provider cannot block it; a fallback
// that left the tunnel would show every name to the provider, and one that
// stayed in it would replace a resolver the user chose, say for blocking ads,
// with another one without a word.

const (
	// directDNSWindow is how far back failed lookups count; directDNSFails
	// of them call for a look at the resolver.
	directDNSWindow = 30 * time.Second
	directDNSFails  = 3
	// directDNSQuiet is how long failures are left alone after a look
	// found the resolver fine: they were of something else (a name that
	// does not resolve anywhere, a moment of poor coverage).
	directDNSQuiet = time.Minute
	// directDNSRecheck is how often a resolver found dead is looked at
	// again; twice as long with the device idle.
	directDNSRecheck = 2 * time.Minute
)

// directDNSWatch follows the user's direct resolver for one connection.
type directDNSWatch struct {
	mu   sync.Mutex
	gen  int
	raw  string // as the settings have it
	host string // what the journal calls it

	fails      []time.Time
	lastKind   dnsprobe.Kind // of the latest failure the journal showed
	looking    bool
	quietUntil time.Time
	down       bool
	cancel     context.CancelFunc // the recheck loop
}

// arm starts watching raw for connection gen.
func (w *directDNSWatch) arm(gen int, raw string) {
	w.disarm()
	srv, err := tunlayer.ParseDNSServer(raw)
	if err != nil {
		return
	}
	w.mu.Lock()
	w.gen, w.raw, w.host = gen, raw, srv.Host
	w.mu.Unlock()
}

// disarm forgets the resolver: the connection is over.
func (w *directDNSWatch) disarm() {
	w.mu.Lock()
	cancel := w.cancel
	w.gen, w.raw, w.host = 0, "", ""
	w.fails, w.lastKind, w.looking, w.quietUntil, w.down, w.cancel = nil, "", false, time.Time{}, false, nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// failure takes a failed lookup of connection gen at now; kind is why, if
// the line said. It returns whether the resolver is to be looked at now.
func (w *directDNSWatch) failure(gen int, now time.Time, kind dnsprobe.Kind) (look bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.raw == "" || w.gen != gen || w.down || w.looking || now.Before(w.quietUntil) {
		return false
	}
	if kind != "" {
		w.lastKind = kind
	}
	w.fails = append(w.fails, now)
	cut := 0
	for cut < len(w.fails) && now.Sub(w.fails[cut]) > directDNSWindow {
		cut++
	}
	w.fails = w.fails[cut:]
	if len(w.fails) < directDNSFails {
		return false
	}
	w.looking, w.fails = true, nil
	return true
}

// failureLine tells whether a line of the TUN layer's journal reports a
// lookup that the direct resolver could not answer, and why. named is set
// when the line names the resolver itself, so that it is the user's and not
// the network's (or the tunnel's) that failed; an unnamed timeout counts as
// well, as sing-box does not say which resolver a silence was.
func failureLine(line, host string) (kind dnsprobe.Kind, named, ok bool) {
	if !strings.Contains(line, "exchange failed for") {
		return "", false, false
	}
	kind = dnsprobe.KindOfText(line)
	if host != "" && (strings.Contains(line, host+":") || strings.Contains(line, "["+host+"]") || strings.Contains(line, "//"+host+"/")) {
		return kind, true, true
	}
	return kind, false, kind == dnsprobe.KindTimeout
}

// noteDirectDNS passes a line of the TUN layer to the watch; it returns
// true for a line about the user's resolver, which the journal leaves to
// the watch's one line.
func (s *Service) noteDirectDNS(line string) bool {
	w := &s.dnsDirect
	w.mu.Lock()
	host, armed := w.host, w.raw != ""
	w.mu.Unlock()
	if !armed {
		return false
	}
	kind, named, ok := failureLine(line, host)
	if !ok {
		return false
	}
	s.mu.Lock()
	gen, state := s.gen, s.status.State
	s.mu.Unlock()
	if state != Connected {
		return named
	}
	if w.failure(gen, time.Now(), kind) {
		go s.lookAtDirectDNS(gen)
	}
	return named
}

// lookAtDirectDNS tries the user's resolver, once failed lookups piled up,
// and says so if it does not answer.
func (s *Service) lookAtDirectDNS(gen int) {
	w := &s.dnsDirect
	w.mu.Lock()
	raw, host, logged := w.raw, w.host, w.lastKind
	w.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*dnsProbeTimeout)
	defer cancel()
	res, skip := s.cfg.dnsProbe(ctx, raw, false)

	w.mu.Lock()
	w.looking = false
	if w.gen != gen {
		w.mu.Unlock()
		return
	}
	switch {
	case skip == "" && res.OK:
		// It answers: the failures were of something else.
		w.quietUntil = time.Now().Add(directDNSQuiet)
		w.mu.Unlock()
		return
	case skip != "":
		// It cannot be tried from here, so nothing is known more than
		// the lookups said.
		res = dnsprobe.Result{Kind: logged}
	case res.Kind == dnsprobe.KindUnsupported:
		res = dnsprobe.Result{Kind: logged}
	}
	if res.Kind == "" {
		res.Kind = dnsprobe.KindOther
	}
	w.mu.Unlock()
	s.directDNSDown(gen, host, res)
}

// directDNSDown declares the user's resolver out of reach for connection
// gen: the status, the journal and the home page say so, and the resolver is
// looked at now and then until it answers.
func (s *Service) directDNSDown(gen int, host string, res dnsprobe.Result) {
	why := whyMsg(res.Kind, res.Err)
	s.mu.Lock()
	if gen != s.gen || s.status.State != Connected {
		s.mu.Unlock()
		return
	}
	s.status.DirectDNS, s.status.DirectDNSWhy = host, &why
	s.mu.Unlock()

	w := &s.dnsDirect
	w.mu.Lock()
	if w.gen != gen || w.down {
		w.mu.Unlock()
		return
	}
	w.down = true
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.mu.Unlock()

	s.hub.publish(Event{Kind: "dns-direct", Reason: "down"}.withLine(msg.New("dns.direct_down", "addr", host, "why", why)))
	go s.recheckDirectDNS(ctx, gen)
}

// recheckDirectDNS looks at the resolver every directDNSRecheck until it
// answers or the connection ends (ctx).
func (s *Service) recheckDirectDNS(ctx context.Context, gen int) {
	w := &s.dnsDirect
	w.mu.Lock()
	raw, host := w.raw, w.host
	w.mu.Unlock()
	for {
		every := s.cfg.dnsRecheck
		if s.idle() {
			every *= 2
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
		res, skip := s.cfg.dnsProbe(ctx, raw, false)
		if skip != "" || !res.OK {
			continue
		}
		s.mu.Lock()
		if gen != s.gen || s.status.State != Connected {
			s.mu.Unlock()
			return
		}
		s.status.DirectDNS, s.status.DirectDNSWhy = "", nil
		s.mu.Unlock()
		w.mu.Lock()
		if w.gen != gen {
			w.mu.Unlock()
			return
		}
		w.down, w.cancel = false, nil
		w.mu.Unlock()
		s.hub.publish(Event{Kind: "dns-direct", Reason: "restored"}.withLine(msg.New("dns.direct_restored", "addr", host)))
		return
	}
}

// directFallback returns the network's resolvers that stand behind the
// user's own direct one (tunlayer.DNSOptions.DirectFallback): none where the
// settings leave the choice to the system, as it is the network's already.
func directFallback(direct string, resolvers []netip.Addr) []netip.Addr {
	if direct == "" {
		return nil
	}
	return resolvers
}
