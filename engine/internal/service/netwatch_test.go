package service

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/supervisor"
)

// netRig takes the harness's network away and gives it back: the route
// table (harness.offline) and, as every check fails without a network, the
// fake cores' health checks (fakecore's offline-file).
type netRig struct {
	h    *harness
	file string
}

func newNetRig(t *testing.T, mutate func(*Config)) (*harness, *netRig) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "offline")
	for _, k := range []string{"XRAY", "SING_BOX", "MIHOMO"} {
		t.Setenv("FAKECORE_"+k, "offline-file:"+file)
	}
	h := newHarness(t, mutate)
	return h, &netRig{h: h, file: file}
}

func (r *netRig) down(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(r.file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.h.offline.Store(true)
}

func (r *netRig) up() {
	r.h.offline.Store(false)
	os.Remove(r.file)
}

// eventsFor collects the harness's events for d.
func eventsFor(h *harness, d time.Duration) []Event {
	var out []Event
	deadline := time.After(d)
	for {
		select {
		case e := <-h.events:
			out = append(out, e)
		case <-deadline:
			return out
		}
	}
}

func kinds(events []Event) []string {
	var out []string
	for _, e := range events {
		k := e.Kind
		if e.Reason != "" {
			k += ":" + e.Reason
		}
		if e.State != "" {
			k += ":" + string(e.State)
		}
		out = append(out, k)
	}
	return out
}

func hasKind(events []Event, kind string) bool { return slices.Contains(kinds(events), kind) }

const namedLink = "trojan://pw@node.example.com:443?sni=node.example.com#Named"

// Connecting without a network does not fail on the server's name: it waits,
// with nothing started, and connects once the network is there.
func TestConnectWaitsForTheNetwork(t *testing.T) {
	h, nw := newNetRig(t, nil)
	nw.down(t)
	if err := h.connect(t, namedLink); err != nil {
		t.Fatalf("connect without a network: %v", err)
	}
	st := h.svc.Status()
	if st.State != NoNetwork || !st.Waiting || st.Node != "Named" || st.Error != "" || st.Core != "" {
		t.Fatalf("status = %+v", st)
	}
	time.Sleep(300 * time.Millisecond)
	if calls := h.log.get(); slices.Contains(calls, "tun.start") || slices.Contains(calls, "dns.apply") {
		t.Errorf("started while waiting: %v", calls)
	}
	h.mu.Lock()
	looked := len(h.lookups)
	h.mu.Unlock()
	if looked != 0 || h.svc.sup.Status().State != supervisor.Idle {
		t.Errorf("waiting, yet %d lookups and the supervisor %s", looked, h.svc.sup.Status().State)
	}
	if st := h.svc.Status(); st.State != NoNetwork {
		t.Fatalf("gave up waiting: %+v", st)
	}

	nw.up()
	st = h.waitState(t, Connected, 10*time.Second)
	if st.Waiting || st.Core != core.Xray || h.guard.active() == nil {
		t.Errorf("status = %+v", st)
	}
	got := kinds(eventsFor(h, 100*time.Millisecond))
	wait, back := slices.Index(got, "network:waiting"), slices.Index(got, "network:back")
	if wait < 0 || back < wait {
		t.Errorf("events = %v", got)
	}
	for _, k := range got {
		if strings.Contains(k, "failed") || k == "error" {
			t.Errorf("a failure while waiting: %v", got)
		}
	}
}

// The wait is a connection under way: disconnecting ends it for good.
func TestDisconnectEndsTheWaitForTheNetwork(t *testing.T) {
	h, nw := newNetRig(t, nil)
	nw.down(t)
	if err := h.connect(t, namedLink); err != nil {
		t.Fatal(err)
	}
	h.svc.Disconnect()
	if st := h.svc.Status(); st.State != Idle {
		t.Fatalf("after disconnect: %+v", st)
	}
	nw.up()
	time.Sleep(500 * time.Millisecond)
	if st := h.svc.Status(); st.State != Idle || slices.Contains(h.log.get(), "tun.start") {
		t.Fatalf("connected after the wait was cancelled: %+v, %v", st, h.log.get())
	}
}

// Another server chosen while waiting replaces the one waited for.
func TestConnectingAnotherServerReplacesTheWait(t *testing.T) {
	h, nw := newNetRig(t, nil)
	nw.down(t)
	if err := h.connect(t, namedLink); err != nil {
		t.Fatal(err)
	}
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if st := h.svc.Status(); st.State != NoNetwork || st.Node != "Trojan" {
		t.Fatalf("status = %+v", st)
	}
	nw.up()
	st := h.waitState(t, Connected, 10*time.Second)
	time.Sleep(300 * time.Millisecond)
	if st.Node != "Trojan" || countOf(h.log.get(), "tun.start") != 1 {
		t.Errorf("status %+v, calls %v", st, h.log.get())
	}
}

// A connection that loses the network is held as it is: no core swapped,
// no server blamed, the tunnel and the DNS redirect in place; it goes on
// when the network returns.
func TestLostNetworkHoldsTheConnection(t *testing.T) {
	h, nw := newNetRig(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	since := h.svc.Status().Since
	from := len(h.log.get())
	nw.down(t)
	st := h.waitState(t, NoNetwork, 5*time.Second)
	if st.Waiting || st.Problem != "" || st.Core != core.Xray {
		t.Errorf("held status = %+v", st)
	}
	// Many checks fail meanwhile, 100ms apart.
	held := eventsFor(h, 1500*time.Millisecond)
	for _, e := range held {
		switch e.Kind {
		case "swap", "core-failed", "no-better", "server", "failover", "core-restart":
			t.Errorf("offline, yet a %s event: %+v", e.Kind, e)
		}
	}
	if !hasKind(held, "network:lost") || !hasKind(held, "state:no-network") {
		t.Errorf("events = %v", kinds(held))
	}
	for _, c := range h.log.get()[from:] {
		if c == "tun.stop" || c == "dns.revert" || c == "tun.start" {
			t.Errorf("the connection was touched while held: %v", h.log.get()[from:])
		}
	}
	if h.guard.active() == nil {
		t.Error("the DNS redirect went while held")
	}
	if st := h.svc.Status(); st.State != NoNetwork || st.Core != core.Xray {
		t.Fatalf("status while held = %+v", st)
	}

	nw.up()
	st = h.waitState(t, Connected, 5*time.Second)
	if !st.Since.Equal(since) {
		t.Errorf("since %v, was %v: the connection is the same one", st.Since, since)
	}
	if calls := h.log.get()[from:]; slices.Contains(calls, "tun.start") {
		t.Errorf("reconnected for a network that came back: %v", calls)
	}
}

// The network is back but nothing gets through the tunnel: after the grace
// the connection is made anew.
func TestHeldConnectionIsMadeAnewWhenNothingGetsThrough(t *testing.T) {
	h, nw := newNetRig(t, func(c *Config) { c.netGrace = 400 * time.Millisecond })
	defer nw.up()
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	nw.down(t)
	h.waitState(t, NoNetwork, 5*time.Second)
	h.offline.Store(false) // the route is back, the checks still fail
	h.waitState(t, Connected, 5*time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for countOf(h.log.get(), "tun.start") < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("not made anew: %v", h.log.get())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A route table that says "no network" while checks get through is wrong:
// the connection goes on, and is not held again for it.
func TestChecksThatPassOverruleTheRouteTable(t *testing.T) {
	h := newHarness(t, nil) // the fake cores stay healthy
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.offline.Store(true)
	// Held, then let go at the next check that passes, 100ms on.
	if e := waitEvent(t, h.events, "network"); e.Reason != "lost" {
		t.Fatalf("event = %+v", e)
	}
	if e := waitEvent(t, h.events, "network"); e.Reason != "back" {
		t.Fatalf("event = %+v", e)
	}
	time.Sleep(500 * time.Millisecond)
	if st := h.svc.Status(); st.State != Connected {
		t.Fatalf("held again: %+v", st)
	}
	if h.svc.offline() {
		t.Error("the supervisor is still told there is no network")
	}
}

// A wait for the network ends when the server's name resolves, whatever
// the route table says.
func TestWaitEndsWhenTheServerResolves(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.netEvidence = 300 * time.Millisecond })
	h.offline.Store(true)
	if err := h.connect(t, namedLink); err != nil {
		t.Fatal(err)
	}
	if st := h.svc.Status(); st.State != NoNetwork || !st.Waiting {
		t.Fatalf("status = %+v", st)
	}
	h.waitState(t, Connected, 5*time.Second)
}

// No server is diagnosed, let alone blamed, without a network.
func TestNoVerdictWithoutANetwork(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.offline.Store(true)
	h.svc.onSupervisorEvent(supervisor.Event{Kind: supervisor.EventNoBetter, Core: core.Xray, Err: errors.New("timeout")})
	for _, e := range eventsFor(h, 300*time.Millisecond) {
		if e.Kind == "server" {
			t.Fatalf("a verdict without a network: %+v", e)
		}
	}
}

// With the network up, a name that does not resolve fails the connection,
// once, in words the user reads.
func TestServerNameThatDoesNotResolve(t *testing.T) {
	old := resolveRetries
	resolveRetries = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	defer func() { resolveRetries = old }()
	h := newHarness(t, func(c *Config) {
		c.lookup = func(_ context.Context, host string, _ netip.AddrPort) (netip.Addr, error) {
			return netip.Addr{}, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
	})
	err := h.connect(t, namedLink)
	if !errors.Is(err, errResolve) || !strings.Contains(err.Error(), "адрес сервера node.example.com не найден в DNS") {
		t.Fatalf("err = %v", err)
	}
	if st := h.svc.Status(); st.State != Failed || st.Error != err.Error() {
		t.Fatalf("status = %+v", st)
	}
	var failed int
	for _, e := range eventsFor(h, 100*time.Millisecond) {
		if e.Kind == "error" || e.Kind == "state" && e.State == Failed {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("the failure was told %d times", failed)
	}
}

func TestResolveErrorWords(t *testing.T) {
	for err, want := range map[error]string{
		&net.DNSError{Err: "no such host", IsNotFound: true}: "не найден в DNS",
		&net.DNSError{Err: "i/o timeout", IsTimeout: true}:   "DNS не ответил",
		context.DeadlineExceeded:                             "DNS не ответил",
		errors.New("server misbehaving"):                     "не удалось узнать адрес сервера h.example: server misbehaving",
	} {
		if got := (&resolveError{host: "h.example", err: err}).Error(); !strings.Contains(got, want) {
			t.Errorf("%v: %q, want %q in it", err, got, want)
		}
	}
}

// The network goes away while the server's name is looked up: the
// connection waits for it rather than failing.
func TestNetworkLostWhileResolving(t *testing.T) {
	var h *harness
	h = newHarness(t, func(c *Config) {
		c.lookup = func(_ context.Context, host string, _ netip.AddrPort) (netip.Addr, error) {
			h.offline.Store(true)
			return netip.Addr{}, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
	})
	if err := h.connect(t, namedLink); err != nil {
		t.Fatalf("err = %v", err)
	}
	if st := h.svc.Status(); st.State != NoNetwork || !st.Waiting {
		t.Fatalf("status = %+v", st)
	}
}

func TestNetHint(t *testing.T) {
	for line, want := range map[string]bool{
		"\x1b[31mERROR\x1b[0m network: missing default interface":                                                  true,
		"ERROR router: process DNS packet: dial UDP connection: dial udp 185.100.100.100:53: no route to internet": true,
		"ERROR dns: lookup failed for x.example: context deadline exceeded":                                        false,
	} {
		if netHint(line) != want {
			t.Errorf("%q: %v", line, !want)
		}
	}
}
