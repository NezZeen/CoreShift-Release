package service

import (
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

// The TUN layer reaches the SOCKS port with the credentials it requires,
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
// credentials to give (browsers cannot): it is open then. With TUN, and on
// Android, it requires them. The core behind it requires credentials of
// its own in every case, which neither the TUN layer nor programs get.
func TestSOCKSPortIsOpenWithoutTUN(t *testing.T) {
	for _, c := range []struct {
		name         string
		tun, android bool
		wantOpen     bool
	}{
		{"TUN", true, false, false},
		{"proxy only", false, false, true},
		{"Android", false, true, false},
	} {
		h := newHarness(t, func(cfg *Config) { cfg.TUN, cfg.AppOutsideVPN = c.tun, c.android })
		if err := h.connect(t, trojanLink); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		get := func(a core.SOCKSAuth, at netip.AddrPort) error {
			cl := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(a.ProxyURL(at))}}
			resp, err := cl.Get("http://health.test/generate_204")
			if err == nil {
				resp.Body.Close()
			}
			return err
		}
		auth := h.svc.sup.SOCKSAuth()
		if err := get(core.SOCKSAuth{}, h.listen); (err == nil) != c.wantOpen {
			t.Errorf("%s: without credentials: %v, want open %v", c.name, err, c.wantOpen)
		}
		if err := get(auth, h.listen); err != nil {
			t.Errorf("%s: with the credentials: %v", c.name, err)
		}

		k := h.svc.Status().Core
		a, _ := core.ByKind(k)
		b, err := os.ReadFile(filepath.Join(h.svc.cfg.DataDir, "work", string(k), a.ConfigName()))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if strings.Contains(string(b), auth.Pass) || !strings.Contains(string(b), "pass") {
			t.Errorf("%s: the %s inbound does not have credentials of its own", c.name, k)
		}
		corePort, _ := h.svc.sup.CoreListen()
		for _, a := range []core.SOCKSAuth{{}, auth} {
			if err := get(a, corePort); err == nil {
				t.Errorf("%s: the core's own port let in a client of the SOCKS port", c.name)
			}
		}
		h.svc.Disconnect()
	}
}
