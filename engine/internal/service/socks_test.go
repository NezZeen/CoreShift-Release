package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coreshift/engine/internal/core"
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

// Without TUN the SOCKS port is the proxy programs are set to use, with no
// credentials to give (browsers cannot): the core's inbound is open then.
// With TUN, and on Android, it keeps them.
func TestSOCKSPortIsOpenWithoutTUN(t *testing.T) {
	for _, c := range []struct {
		name          string
		tun, android  bool
		wantPasswords bool
	}{
		{"TUN", true, false, true},
		{"proxy only", false, false, false},
		{"Android", false, true, true},
	} {
		h := newHarness(t, func(cfg *Config) { cfg.TUN, cfg.AppOutsideVPN = c.tun, c.android })
		if err := h.connect(t, trojanLink); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		k := h.svc.Status().Core
		a, _ := core.ByKind(k)
		b, err := os.ReadFile(filepath.Join(h.svc.cfg.DataDir, "work", string(k), a.ConfigName()))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := strings.Contains(string(b), h.svc.sup.SOCKSAuth().Pass); got != c.wantPasswords {
			t.Errorf("%s: the %s inbound requires credentials: %v, want %v", c.name, k, got, c.wantPasswords)
		}
		h.svc.Disconnect()
	}
}
