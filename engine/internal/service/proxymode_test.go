package service

import (
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/store"
)

// fetchThrough gets the fake cores' health URL through the proxy at proxy.
func fetchThrough(proxy *url.URL) error {
	cl := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	resp, err := cl.Get("http://health.test/generate_204")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return &url.Error{Op: "get", URL: proxy.Redacted(), Err: errStatus(resp.StatusCode)}
	}
	return nil
}

type errStatus int

func (e errStatus) Error() string { return http.StatusText(int(e)) }

func httpProxyURL(at netip.AddrPort, a core.SOCKSAuth) *url.URL {
	u := &url.URL{Scheme: "http", Host: at.String()}
	if a.Set() {
		u.User = url.UserPassword(a.User, a.Pass)
	}
	return u
}

// Without the TUN layer the port is an HTTP proxy too, which the system
// proxy needs: open on the desktop. With the TUN layer only the layer
// uses the port, by SOCKS.
func TestHTTPProxyWithoutTUN(t *testing.T) {
	for _, c := range []struct {
		name     string
		tun      bool
		wantHTTP bool
	}{
		{"TUN", true, false},
		{"proxy only", false, true},
	} {
		h := newHarness(t, func(cfg *Config) { cfg.TUN = c.tun })
		if err := h.connect(t, trojanLink); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err := fetchThrough(httpProxyURL(h.listen, core.SOCKSAuth{})); (err == nil) != c.wantHTTP {
			t.Errorf("%s: HTTP without credentials: %v, want served %v", c.name, err, c.wantHTTP)
		}
		h.svc.Disconnect()
	}
}

// Android's proxy without the VPN: any app on the phone reaches 127.0.0.1,
// so the port takes the user's credentials and the engine's own, and
// nothing else; HTTP without them only when the user allowed it. The
// journal says the proxy is up and down, without the password.
func TestAndroidProxyNeedsTheUsersCredentials(t *testing.T) {
	user := core.SOCKSAuth{User: "csphone", Pass: "S3cretPassw0rdForApps"}
	for _, openHTTP := range []bool{false, true} {
		h := newHarness(t, func(cfg *Config) {
			cfg.TUN, cfg.AppOutsideVPN = false, true
			cfg.ProxyAuth, cfg.ProxyOpenHTTP = user, openHTTP
		})
		if err := h.connect(t, trojanLink); err != nil {
			t.Fatal(err)
		}
		if err := fetchThrough(user.ProxyURL(h.listen)); err != nil {
			t.Errorf("SOCKS with the user's credentials: %v", err)
		}
		if err := fetchThrough(httpProxyURL(h.listen, user)); err != nil {
			t.Errorf("HTTP with the user's credentials: %v", err)
		}
		if err := fetchThrough(h.svc.sup.SOCKSAuth().ProxyURL(h.listen)); err != nil {
			t.Errorf("the engine's own clients: %v", err)
		}
		for name, a := range map[string]core.SOCKSAuth{"no": {}, "wrong": {User: user.User, Pass: "guess"}} {
			if err := fetchThrough(a.ProxyURL(h.listen)); err == nil {
				t.Errorf("SOCKS with %s credentials let in", name)
			}
		}
		if err := fetchThrough(httpProxyURL(h.listen, core.SOCKSAuth{})); (err == nil) != openHTTP {
			t.Errorf("HTTP without credentials, open HTTP %v: %v", openHTTP, err)
		}
		h.svc.Disconnect()
		var up, down bool
		for e := range drainEvents(h) {
			if strings.Contains(e, user.Pass) || strings.Contains(e, user.User) {
				t.Errorf("an event carries the credentials: %s", e)
			}
			up = up || strings.HasPrefix(e, "proxy прокси без VPN на ")
			down = down || strings.HasPrefix(e, "proxy прокси без VPN выключен")
		}
		if !up || !down {
			t.Errorf("open HTTP %v: the journal did not say the proxy went up (%v) and down (%v)", openHTTP, up, down)
		}
	}
}

// TUN on Android is as it was: the port takes only the engine's
// credentials, the user's are not let in, and no HTTP.
func TestAndroidTUNUnchanged(t *testing.T) {
	user := core.SOCKSAuth{User: "csphone", Pass: "S3cretPassw0rdForApps"}
	h := newHarness(t, func(cfg *Config) {
		cfg.TUN, cfg.AppOutsideVPN = true, true
		cfg.ProxyAuth, cfg.ProxyOpenHTTP = user, true
	})
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if err := fetchThrough(user.ProxyURL(h.listen)); err == nil {
		t.Error("the user's credentials let in with the TUN layer")
	}
	if err := fetchThrough(httpProxyURL(h.listen, core.SOCKSAuth{})); err == nil {
		t.Error("HTTP served with the TUN layer")
	}
	for e := range drainEvents(h) {
		if strings.HasPrefix(e, "proxy ") {
			t.Errorf("a proxy event with the TUN layer: %s", e)
		}
	}
}

// The credentials are made once, on Android only, and kept.
func TestProxyCredentialsMadeOnce(t *testing.T) {
	open := func(dir string) *store.Store {
		st, err := OpenStore(dir, store.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	dir := t.TempDir()
	st := open(dir)
	ensureProxyAuth(st, false)
	if st.Settings().Proxy.HasAuth() {
		t.Fatal("the desktop got credentials it never uses")
	}
	ensureProxyAuth(st, true)
	first := st.Settings().Proxy
	if !first.HasAuth() || len(first.Pass) < 16 {
		t.Fatalf("credentials %q/%d characters", first.User, len(first.Pass))
	}
	ensureProxyAuth(open(dir), true)
	if again := open(dir).Settings().Proxy; again != first {
		t.Errorf("credentials changed at the next start")
	}
	o := OptionsFromSettings(open(dir).Settings())
	if o.ProxyAuth.User != first.User || o.ProxyAuth.Pass != first.Pass {
		t.Error("the options do not carry the credentials")
	}
}

// Turning the system proxy on or off while connected is no reason to
// reconnect: the app sets it at once. The credentials are.
func TestSystemProxyIsNotPending(t *testing.T) {
	h := newHarness(t, func(cfg *Config) { cfg.TUN = false })
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	o := h.svc.Options()
	o.SystemProxy = !o.SystemProxy
	h.svc.SetOptions(o)
	if h.svc.Status().Pending {
		t.Error("the system proxy marked the connection pending")
	}
	o.ProxyOpenHTTP = !o.ProxyOpenHTTP
	h.svc.SetOptions(o)
	if !h.svc.Status().Pending {
		t.Error("a change of the proxy's access did not mark it pending")
	}
}
