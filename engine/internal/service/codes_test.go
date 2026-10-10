package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"coreshift/engine/internal/msg"
)

// A server's name that does not resolve fails the connection with a code
// the app words itself; Error keeps the Russian for older apps.
func TestFailedConnectionCarriesTheCode(t *testing.T) {
	for _, c := range []struct {
		err  error
		code string
	}{
		{&net.DNSError{Err: "no such host", Name: "vpn.example", IsNotFound: true}, "server.resolve.not_found"},
		{&net.DNSError{Err: "timeout", Name: "vpn.example", IsTimeout: true}, "server.resolve.timeout"},
		{context.DeadlineExceeded, "server.resolve.timeout"},
		{errors.New("weird"), "server.resolve.failed"},
	} {
		err := error(&resolveError{host: "vpn.example", err: c.err})
		m, ok := msg.Of(err)
		if !ok || m.Code != c.code || m.Args["host"] != "vpn.example" {
			t.Errorf("%v: %+v %v", c.err, m, ok)
		}
		if !errors.Is(err, errResolve) || err.Error() != m.String() || !strings.Contains(err.Error(), "vpn.example") {
			t.Errorf("%v: text %q", c.err, err.Error())
		}
		s := &Service{hub: newHub()}
		events, stop := s.hub.subscribe(false)
		s.fail(err)
		stop()
		e := <-events
		st := s.Status()
		if e.Kind != "state" || e.Code != c.code || e.Error != err.Error() || st.ErrorCode != c.code || st.ErrorArgs["host"] != "vpn.example" {
			t.Errorf("%v: event %+v, status %+v", c.err, e, st)
		}
	}
	// A plain error goes as it is, without a code.
	s := &Service{hub: newHub()}
	s.fail(errors.New("start TUN layer: boom"))
	if st := s.Status(); st.ErrorCode != "" || st.Error != "start TUN layer: boom" {
		t.Errorf("plain: %+v", st)
	}
	b, _ := json.Marshal(Status{State: Failed, Error: "x", ErrorCode: "server.resolve.timeout", ErrorArgs: map[string]any{"host": "h"}})
	if !strings.Contains(string(b), `"error_code":"server.resolve.timeout","error_args":{"host":"h"}`) {
		t.Errorf("json %s", b)
	}
}

// The proxy's journal lines go as codes, with the address.
func TestProxyLinesCarryCodes(t *testing.T) {
	addr := netip.MustParseAddrPort("127.0.0.1:17890")
	s := &Service{hub: newHub(), cfg: Config{Listen: addr}}
	events, stop := s.hub.subscribe(false)
	defer stop()
	s.proxyUp(Options{SystemProxy: true})
	s.proxyDown()
	up, down := <-events, <-events
	if up.Code != "proxy.up.system" || up.Args["addr"] != addr.String() || !strings.Contains(up.Line, "127.0.0.1:17890 открыт") {
		t.Errorf("up %+v", up)
	}
	if down.Code != "proxy.down" || down.Line != "прокси закрыт" {
		t.Errorf("down %+v", down)
	}
}

// What the engine says itself goes with its code and its Russian.
func TestEventWithCode(t *testing.T) {
	e := Event{Kind: "dns"}.withError(msg.New("dns.revert_failed", "err", errors.New("denied")))
	if e.Code != "dns.revert_failed" || e.Error != "не удалось восстановить системный DNS: denied" || e.Line != "" {
		t.Errorf("event %+v", e)
	}
	b, _ := json.Marshal(e)
	if !strings.Contains(string(b), `"code":"dns.revert_failed","args":{"err":{"code":"raw","args":{"text":"denied"}}}`) {
		t.Errorf("json %s", b)
	}
}
