package socksgate

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/core"
)

// httpProxy is the gate as an HTTP proxy, with a's credentials when set.
func httpProxy(g *Gate, a core.SOCKSAuth) *url.URL {
	u := &url.URL{Scheme: "http", Host: g.Addr().String()}
	if a.Set() {
		u.User = url.UserPassword(a.User, a.Pass)
	}
	return u
}

// getTLS fetches srv, an httptest TLS server, through proxy: a CONNECT.
func getTLS(proxy *url.URL, srv *httptest.Server) error {
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.Proxy, tr.DisableKeepAlives = http.ProxyURL(proxy), true
	c := &http.Client{Timeout: 5 * time.Second, Transport: tr}
	resp, err := c.Get(srv.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err == nil && string(b) != "hello" {
		err = fmt.Errorf("got %q", b)
	}
	return err
}

func tlsServer(t testing.TB) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") }))
	t.Cleanup(srv.Close)
	return srv
}

// The open gate of the desktop without the TUN layer is an HTTP proxy too,
// on the same port: plain http:// and CONNECT, which is what the system
// proxy of Windows sends.
func TestHTTPOpen(t *testing.T) {
	web, secure := webServer(t), tlsServer(t)
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth, Open: true, HTTP: true})
	g.SetTarget(f.target())
	if err := get(httpProxy(g, core.SOCKSAuth{}), web.URL); err != nil {
		t.Fatalf("plain http: %v", err)
	}
	if err := getTLS(httpProxy(g, core.SOCKSAuth{}), secure); err != nil {
		t.Fatalf("CONNECT: %v", err)
	}
	// SOCKS still works on the same port.
	if err := get(core.SOCKSAuth{}.ProxyURL(g.Addr()), web.URL); err != nil {
		t.Fatalf("SOCKS beside HTTP: %v", err)
	}
	if tr := g.Traffic(); tr.Down == 0 || tr.Up == 0 {
		t.Errorf("HTTP traffic not counted: %+v", tr)
	}
}

// A request in origin form is what a web page makes the browser send to
// 127.0.0.1: it is not a proxy request and never reaches the core.
func TestHTTPOriginFormRefused(t *testing.T) {
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth, Open: true, HTTP: true})
	g.SetTarget(f.target())
	for _, req := range []string{
		"GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n",
		"POST /x HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Length: 0\r\n\r\n",
		"GET https://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n",
		"CONNECT example.com HTTP/1.1\r\nHost: example.com\r\n\r\n",
	} {
		if code := rawHTTP(t, g, req); code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", strings.SplitN(req, "\r\n", 2)[0], code)
		}
	}
	if n := f.reqs.Load(); n != 0 {
		t.Errorf("the core got %d requests", n)
	}
}

// rawHTTP sends req to the gate and returns the status of the answer.
func rawHTTP(t *testing.T, g *Gate, req string) int {
	t.Helper()
	c, err := net.Dial("tcp", g.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Without the TUN layer on Android every app on the phone can reach the
// port: the gate takes the user's credentials (Guest) besides the
// engine's own, for SOCKS and as HTTP proxy credentials, and nothing
// without them.
func TestGuestCredentials(t *testing.T) {
	web, secure := webServer(t), tlsServer(t)
	f := startFakeCore(t, coreAuth)
	guest := core.SOCKSAuth{User: "phone", Pass: "guest-secret"}
	g := newGate(t, Config{Auth: clientAuth, Guest: guest, HTTP: true})
	g.SetTarget(f.target())
	for name, a := range map[string]core.SOCKSAuth{"guest": guest, "own": clientAuth} {
		if err := get(a.ProxyURL(g.Addr()), web.URL); err != nil {
			t.Errorf("SOCKS with the %s credentials: %v", name, err)
		}
		if err := get(httpProxy(g, a), web.URL); err != nil {
			t.Errorf("HTTP with the %s credentials: %v", name, err)
		}
		if err := getTLS(httpProxy(g, a), secure); err != nil {
			t.Errorf("CONNECT with the %s credentials: %v", name, err)
		}
	}
	for name, a := range map[string]core.SOCKSAuth{
		"no":                   {},
		"wrong":                {User: guest.User, Pass: "wrong"},
		"mixed":                {User: guest.User, Pass: clientAuth.Pass},
		"the core's":           coreAuth,
		"the guest's, no pass": {User: guest.User},
	} {
		if err := get(a.ProxyURL(g.Addr()), web.URL); err == nil {
			t.Errorf("SOCKS with %s credentials: let through", name)
		}
		if err := get(httpProxy(g, a), web.URL); err == nil {
			t.Errorf("HTTP with %s credentials: let through", name)
		}
	}
	if code := rawHTTP(t, g, "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n"); code != http.StatusProxyAuthRequired {
		t.Errorf("HTTP without credentials: %d, want 407", code)
	}
}

// A gate without Guest takes no empty credentials for them.
func TestNoGuest(t *testing.T) {
	web := webServer(t)
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth, HTTP: true})
	g.SetTarget(f.target())
	if err := get(core.SOCKSAuth{User: "x"}.ProxyURL(g.Addr()), web.URL); err == nil {
		t.Error("SOCKS with made-up credentials let through")
	}
}

// OpenHTTP: Android's Wi-Fi proxy setting cannot give credentials, so HTTP
// may be let in without them, while SOCKS still needs them.
func TestOpenHTTPOnly(t *testing.T) {
	web := webServer(t)
	f := startFakeCore(t, coreAuth)
	guest := core.SOCKSAuth{User: "phone", Pass: "guest-secret"}
	g := newGate(t, Config{Auth: clientAuth, Guest: guest, HTTP: true, OpenHTTP: true})
	g.SetTarget(f.target())
	if err := get(httpProxy(g, core.SOCKSAuth{}), web.URL); err != nil {
		t.Fatalf("HTTP without credentials: %v", err)
	}
	if err := get(core.SOCKSAuth{}.ProxyURL(g.Addr()), web.URL); err == nil {
		t.Error("SOCKS without credentials let through")
	}
}

// Without HTTP (the TUN layer's gate) an HTTP request is not served.
func TestHTTPOff(t *testing.T) {
	web := webServer(t)
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth, Open: true})
	g.SetTarget(f.target())
	if err := get(httpProxy(g, core.SOCKSAuth{}), web.URL); err == nil {
		t.Error("HTTP served by a gate without HTTP")
	}
}

func TestConnectRequest(t *testing.T) {
	for target, want := range map[string]string{
		"example.com:443": "05 01 00 03 0b 65 78 61 6d 70 6c 65 2e 63 6f 6d 01 bb",
		"1.2.3.4:80":      "05 01 00 01 01 02 03 04 00 50",
		"[::1]:8080":      "05 01 00 04 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 01 1f 90",
	} {
		b, err := connectRequest(target)
		if err != nil || fmt.Sprintf("% x", b) != want {
			t.Errorf("%s: % x, %v; want %s", target, b, err, want)
		}
	}
	for _, bad := range []string{"example.com", "example.com:0", "example.com:70000", ":80"} {
		if _, err := connectRequest(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}
