package service

import (
	"net/http"
	"strings"
	"testing"

	"coreshift/engine/internal/store"
)

// Ten test cores at once on the desktop, fewer on a phone.
func TestCoreConcurrency(t *testing.T) {
	for goos, want := range map[string]int{"windows": 10, "linux": 10, "darwin": 10, "android": 3} {
		if got := coreConcurrency(goos); got != want {
			t.Errorf("%s: %d test cores at once, want %d", goos, got, want)
		}
	}
	if latencyConcurrency != 10 {
		t.Errorf("latencyConcurrency = %d, want 10", latencyConcurrency)
	}
}

// In the mode through the server the delay shown is the core test's alone.
// A server whose port answers but whose core test fails is a failure, red,
// with no number from the handshake; the handshake only makes its error say
// more.
func TestLatencyProxyModeNeverReportsTCPAsLatency(t *testing.T) {
	for _, k := range []string{"XRAY", "SING_BOX", "MIHOMO"} {
		t.Setenv("FAKECORE_"+k, "unhealthy")
	}
	h, srv, _ := latencyRig(t)
	chooseLatencyTest(t, h, srv, store.LatencyProxy)
	// The rig's handshakes answer on port 443 and fail on the others; its
	// ICMP always fails.
	links := []string{
		"trojan://pw@203.0.113.6:443?sni=a.example.com#PortOpen",
		"trojan://pw@203.0.113.6:1002?sni=t.example.com#PortRefused",
		"hysteria2://pw@203.0.113.9:443?sni=t.example.com#UDP",
	}
	var sub subscriptionView
	callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": strings.Join(links, "\n")}, &sub)
	for len(h.events) > 0 {
		<-h.events
	}
	var res []NodeLatency
	if code := callJSON(t, srv, "POST", "/v1/latency", map[string]string{"subscription": sub.ID}, &res); code != http.StatusOK || len(res) != len(links) {
		t.Fatalf("latency: %d %+v", code, res)
	}
	for i, r := range res {
		if r.Method != methodProxy || r.LatencyMS != 0 || r.Error == "" {
			t.Errorf("%s: %+v, want a failure through the core", links[i], r)
		}
	}
	if !strings.Contains(res[0].Error, "the port is open") || !strings.HasPrefix(res[0].Error, "proxy: ") {
		t.Errorf("an open port is not told in the error: %q", res[0].Error)
	}
	if !strings.HasPrefix(res[1].Error, "ping: ") || !strings.Contains(res[1].Error, "; proxy: ") {
		t.Errorf("a refused port is not told in the error: %q", res[1].Error)
	}
	// Whatever was published or stored carries no handshake's number.
	for len(h.events) > 0 {
		if e := <-h.events; e.Kind == "latency" && e.Fingerprint != "" && (e.LatencyMS != 0 || e.Method != methodProxy) {
			t.Errorf("event %+v", e)
		}
	}
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &sub)
	for _, n := range sub.Nodes {
		if n.LatencyMS != 0 || n.LatencyError == "" || n.LatencyMethod != methodProxy {
			t.Errorf("node view: %+v", n)
		}
	}
}
