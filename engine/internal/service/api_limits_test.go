package service

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// startAPIServer serves the API with NewAPIServer, as the daemon does, on a
// free loopback port, with the limits shrunk by shrink for the test.
func startAPIServer(t *testing.T, shrink func()) (*harness, string) {
	t.Helper()
	saved := apiLimits
	t.Cleanup(func() { apiLimits = saved })
	if shrink != nil {
		shrink()
	}
	h := newHarness(t, func(c *Config) { c.TUN = false })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddrPort(ln.Addr().String())
	srv := NewAPIServer(NewAPI(h.svc, token, addr))
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return h, addr.String()
}

// closedWithin reports whether the server closes c within d.
func closedWithin(c net.Conn, d time.Duration) bool {
	c.SetReadDeadline(time.Now().Add(d))
	_, err := io.Copy(io.Discard, c)
	return err == nil // EOF: closed by the server, not our deadline
}

func TestAPIServerTimeouts(t *testing.T) {
	_, addr := startAPIServer(t, func() {
		apiLimits.readHeader = 200 * time.Millisecond
		apiLimits.read = 400 * time.Millisecond
		apiLimits.idle = 300 * time.Millisecond
	})
	dial := func() net.Conn {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}

	// Headers that never end.
	c := dial()
	io.WriteString(c, "GET /v1/status HTTP/1.1\r\nHost: "+addr+"\r\n")
	if !closedWithin(c, 3*time.Second) {
		t.Error("a request whose headers never end is not cut off")
	}

	// A body that never comes.
	c = dial()
	io.WriteString(c, "PUT /v1/selection HTTP/1.1\r\nHost: "+addr+"\r\nAuthorization: Bearer "+token+
		"\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{")
	if !closedWithin(c, 3*time.Second) {
		t.Error("a request whose body never comes is not cut off")
	}

	// An idle keep-alive connection.
	c = dial()
	io.WriteString(c, "GET /v1/status HTTP/1.1\r\nHost: "+addr+"\r\nAuthorization: Bearer "+token+"\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %v %v", resp, err)
	}
	io.Copy(io.Discard, resp.Body)
	if !closedWithin(c, 3*time.Second) {
		t.Error("an idle connection is kept open")
	}
}

func TestAPIServerHeaderLimit(t *testing.T) {
	_, addr := startAPIServer(t, nil)
	req, _ := http.NewRequest("GET", "http://"+addr+"/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Padding", strings.Repeat("a", 2*apiLimits.maxHeader))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Errorf("huge headers: %d", resp.StatusCode)
	}
}

// openStream opens the event stream and returns its lines.
func openStream(t *testing.T, ctx context.Context, addr string) (<-chan string, *http.Response) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event stream: %d", resp.StatusCode)
	}
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return lines, resp
}

// The read timeout cuts off slow requests, not the event stream, which
// stays open as long as the app runs.
func TestAPIEventStreamOutlivesReadTimeout(t *testing.T) {
	h, addr := startAPIServer(t, func() { apiLimits.read = 200 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines, resp := openStream(t, ctx, addr)
	defer resp.Body.Close()
	time.Sleep(4 * apiLimits.read)
	h.svc.hub.publish(Event{Kind: "log", Line: "still here"})
	deadline := time.After(3 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("the event stream ended at the read timeout")
			}
			if strings.Contains(l, "still here") {
				return
			}
		case <-deadline:
			t.Fatal("no event after the read timeout")
		}
	}
}

// Beyond maxStreams the oldest stream ends and the new one works: an app
// reconnecting is never locked out by its own stale stream.
func TestAPIEventStreamsAreCapped(t *testing.T) {
	h, addr := startAPIServer(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var first <-chan string
	for i := range maxStreams {
		lines, resp := openStream(t, ctx, addr)
		defer resp.Body.Close()
		if i == 0 {
			first = lines
		}
	}
	lines, resp := openStream(t, ctx, addr)
	defer resp.Body.Close()

	// The oldest ends.
	timeout := time.After(3 * time.Second)
	for ended := false; !ended; {
		select {
		case _, ok := <-first:
			ended = !ok
		case <-timeout:
			t.Fatal("the oldest stream was not ended")
		}
	}
	// The newest gets events.
	h.svc.hub.publish(Event{Kind: "log", Line: "newest"})
	for {
		select {
		case l := <-lines:
			if strings.Contains(l, "newest") {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the newest stream gets no events")
		}
	}
}

func TestStreamsEvictOldest(t *testing.T) {
	s := &streams{max: 2}
	var ended [3]bool
	removes := make([]func(), 3)
	for i := range 3 {
		removes[i] = s.add(func() { ended[i] = true })
	}
	if !ended[0] || ended[1] || ended[2] {
		t.Errorf("ended %v, want only the first", ended)
	}
	if s.count() != 2 {
		t.Errorf("%d open, want 2", s.count())
	}
	removes[0]() // already gone: nothing happens
	removes[2]()
	if s.count() != 1 {
		t.Errorf("%d open after removing one, want 1", s.count())
	}
}

func TestAPIBodyLimits(t *testing.T) {
	_, srv := newStoreAPI(t, nil)
	big := `{"subscription": "` + strings.Repeat("a", smallBody) + `"}`
	if code, _ := call(t, srv, "PUT", "/v1/selection", big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("a large body to a small route: %d", code)
	}
	// Settings may be large, up to maxBody.
	settings := `{"ipv6": true, "x": "` + strings.Repeat("a", 2*smallBody) + `"}`
	if code, out := call(t, srv, "PUT", "/v1/settings", settings); code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "unknown field") {
		t.Errorf("a large settings body: %d %v", code, out)
	}
	huge := `{"content": "` + strings.Repeat("a", maxBody) + `"}`
	if code, _ := call(t, srv, "POST", "/v1/subscription/parse", huge); code != http.StatusRequestEntityTooLarge {
		t.Errorf("a body over maxBody: %d", code)
	}
	for _, p := range []string{"POST /v1/connect", "POST /v1/subscriptions", "PUT /v1/settings", "POST /v1/subscription/parse"} {
		if bodyLimit(p) != maxBody {
			t.Errorf("%s takes %d", p, bodyLimit(p))
		}
	}
	if bodyLimit("POST /v1/latency") != smallBody || bodyLimit("") != smallBody {
		t.Error("small routes take more than smallBody")
	}
}

func TestAPIProvesItKnowsTheToken(t *testing.T) {
	_, srv := newAPIServer(t)
	get := func(tok, nonce string) *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/hello", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		if nonce != "" {
			req.Header.Set(NonceHeader, nonce)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	a, b := get(token, "nonce-a"), get(token, "nonce-b")
	if a.StatusCode != http.StatusOK || a.Header.Get(ProofHeader) != APIProof(token, "nonce-a") {
		t.Errorf("proof %q, want %q", a.Header.Get(ProofHeader), APIProof(token, "nonce-a"))
	}
	if b.Header.Get(ProofHeader) == a.Header.Get(ProofHeader) {
		t.Error("the proof does not depend on the nonce")
	}
	if APIProof("other-token", "nonce-a") == APIProof(token, "nonce-a") {
		t.Error("the proof does not depend on the token")
	}
	if r := get("wrong", "nonce-a"); r.StatusCode != http.StatusUnauthorized || r.Header.Get(ProofHeader) != "" {
		t.Errorf("without the token: %d, proof %q", r.StatusCode, r.Header.Get(ProofHeader))
	}
	if r := get(token, ""); r.Header.Get(ProofHeader) != "" {
		t.Error("a proof without a nonce")
	}
	if r := get(token, strings.Repeat("n", maxNonce+1)); r.Header.Get(ProofHeader) != "" {
		t.Error("a proof for an over-long nonce")
	}
	// The app checks the same value (app/lib/api/proof.dart): a fixed vector.
	if got := APIProof("key", "abc"); got != "60d75857ca50818b2d685ed7b41542c662fdb4b4716ba8c84aa43345918615e6" {
		t.Errorf("APIProof(key, abc) = %s", got)
	}
}
