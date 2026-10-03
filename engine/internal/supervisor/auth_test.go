package supervisor

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
)

// Every core the supervisor starts requires the credentials, and its own
// health checks and latency tests use them. The fake core enforces them as
// the real ones do once the config has them.
func TestCoresRequireSOCKSCredentials(t *testing.T) {
	for _, k := range []core.Kind{core.Xray, core.SingBox, core.Mihomo} {
		t.Run(string(k), func(t *testing.T) {
			h := newHarness(t, func(c *Config) { c.Mode, c.ManualCore = Manual, k })
			auth := h.s.SOCKSAuth()
			if !auth.Set() {
				t.Fatal("no credentials made")
			}
			if err := h.s.Connect(context.Background(), mustNode(t, trojanLink), ""); err != nil {
				t.Fatal(err)
			}
			// The health check gets through: it knows the credentials.
			h.waitFor(t, "a passing health check", 10*time.Second, func(e Event) bool {
				return e.Kind == EventHealth && e.Err == nil
			})
			get := func(c *http.Client) error {
				resp, err := c.Get("http://health.test/generate_204")
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				return err
			}
			plain := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(core.SOCKSAuth{}.ProxyURL(h.listen))}}
			if err := get(plain); err == nil {
				t.Error("the inbound served a client without credentials")
			}
			wrong := core.SOCKSAuth{User: auth.User, Pass: "wrong"}
			if err := get(&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(wrong.ProxyURL(h.listen))}}); err == nil {
				t.Error("the inbound took a wrong password")
			}
			if err := get(&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(auth.ProxyURL(h.listen))}}); err != nil {
				t.Errorf("with the credentials: %v", err)
			}
		})
	}
}

func TestLatencyTestUsesCredentials(t *testing.T) {
	h := newHarness(t, nil)
	var got []LatencyResult
	h.s.TestLatency(context.Background(), []node.Node{mustNode(t, trojanLink)}, nil, 2, func(r LatencyResult) { got = append(got, r) })
	if len(got) != 1 || got[0].Err != nil {
		t.Fatalf("results %+v", got)
	}
}
