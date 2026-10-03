package service

import (
	"strings"
	"testing"
)

// The TUN layer reaches the core with the credentials the core requires,
// and they are new for each service.
func TestTUNLayerGetsTheSOCKSCredentials(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	h.tun.mu.Lock()
	o := h.tun.opts
	h.tun.mu.Unlock()
	auth := h.svc.sup.SOCKSAuth()
	if o.UpstreamUser == "" || o.UpstreamUser != auth.User || o.UpstreamPass != auth.Pass {
		t.Errorf("TUN upstream credentials do not match the cores'")
	}
	if u := h.svc.proxyURL(); u.User.Username() != auth.User || u.Host != h.listen.String() {
		t.Errorf("the service's own proxy is %s", u.Redacted())
	}
	other := newHarness(t, nil)
	if other.svc.socks == h.svc.socks {
		t.Error("two services share credentials")
	}
	// Nothing the service prints carries them.
	for e := range drainEvents(h) {
		if strings.Contains(e, auth.Pass) {
			t.Errorf("an event carries the password: %s", e)
		}
	}
}

// drainEvents returns what the events so far would print.
func drainEvents(h *harness) map[string]bool {
	out := map[string]bool{}
	for {
		select {
		case e := <-h.events:
			out[e.Kind+" "+e.Line+" "+e.Error+" "+e.Reason] = true
		default:
			return out
		}
	}
}
