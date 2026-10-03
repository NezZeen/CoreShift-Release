package service

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

const token = "test-token"

func newAPIServer(t *testing.T) (*harness, *httptest.Server) {
	t.Helper()
	h := newHarness(t, func(c *Config) { c.TUN = false })
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = NewAPI(h.svc, token, netip.MustParseAddrPort(srv.Listener.Addr().String()))
	srv.Start()
	t.Cleanup(srv.Close)
	return h, srv
}

func call(t *testing.T, srv *httptest.Server, method, path, body string, mutate ...func(*http.Request)) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for _, m := range mutate {
		m(req)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	b, _ := io.ReadAll(resp.Body)
	json.Unmarshal(b, &out)
	return resp.StatusCode, out
}

func TestAPIRejectsStrangers(t *testing.T) {
	_, srv := newAPIServer(t)
	if code, _ := call(t, srv, "GET", "/v1/status", "", func(r *http.Request) { r.Header.Del("Authorization") }); code != http.StatusUnauthorized {
		t.Errorf("no token: %d", code)
	}
	if code, _ := call(t, srv, "GET", "/v1/status", "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }); code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", code)
	}
	// DNS rebinding: a page on evil.example resolving to 127.0.0.1.
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	if code, _ := call(t, srv, "GET", "/v1/status", "", func(r *http.Request) { r.Host = "evil.example:" + port }); code != http.StatusForbidden {
		t.Errorf("foreign host: %d", code)
	}
}

func TestAPIConnectFlow(t *testing.T) {
	_, srv := newAPIServer(t)

	req, _ := http.NewRequest("GET", srv.URL+"/v1/events?replay=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	stream, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	gotConnected := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			var e Event
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok && json.Unmarshal([]byte(data), &e) == nil &&
				e.Kind == "state" && e.State == Connected {
				close(gotConnected)
				return
			}
		}
	}()

	code, st := call(t, srv, "POST", "/v1/connect", `{"link":"`+trojanLink+`"}`)
	if code != http.StatusOK || st["state"] != "connected" || st["core"] != "xray" {
		t.Fatalf("connect: %d %v", code, st)
	}
	select {
	case <-gotConnected:
	case <-time.After(5 * time.Second):
		t.Error("no connected event on the stream")
	}
	if code, st := call(t, srv, "POST", "/v1/disconnect", ""); code != http.StatusOK || st["state"] != "idle" {
		t.Errorf("disconnect: %d %v", code, st)
	}
	if code, out := call(t, srv, "POST", "/v1/connect", `{"link":"vless://broken"}`); code != http.StatusBadRequest || out["error"] == nil {
		t.Errorf("bad link: %d %v", code, out)
	}
}

func TestAPIParseSubscription(t *testing.T) {
	_, srv := newAPIServer(t)
	content := trojanLink + "\nhy2://auth@203.0.113.10:443/?sni=h.example.com&insecure=1#Hy2\nsocks://a@203.0.113.1:1#Socks"
	body, _ := json.Marshal(map[string]string{"content": content})
	code, out := call(t, srv, "POST", "/v1/subscription/parse", string(body))
	if code != http.StatusOK {
		t.Fatalf("parse: %d %v", code, out)
	}
	nodes := out["nodes"].([]any)
	if len(nodes) != 2 || len(out["skipped"].([]any)) != 1 {
		t.Fatalf("nodes=%d skipped=%v", len(nodes), out["skipped"])
	}
	hy2 := nodes[1].(map[string]any)
	cores := hy2["cores"].([]any)
	if len(cores) != 2 || cores[0] != "sing-box" || hy2["fingerprint"] == "" {
		t.Errorf("hysteria2 entry = %v", hy2)
	}
}
