package socksgate

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The guard of the system proxy tells CoreShift's gate from another
// program that took the port: only the gate answers the probe, with or
// without a core and whatever credentials it takes.
func TestIsGate(t *testing.T) {
	open := newGate(t, Config{Auth: clientAuth, Open: true, HTTP: true})
	if !IsGate(open.Addr().String(), 2*time.Second) {
		t.Error("the open gate (no core yet) not recognised")
	}
	locked := newGate(t, Config{Auth: clientAuth, HTTP: true})
	if !IsGate(locked.Addr().String(), 2*time.Second) {
		t.Error("the gate that needs credentials not recognised")
	}
	// The TUN layer's gate has no HTTP: the system proxy is not CoreShift's
	// business then, and restoring it is right.
	tun := newGate(t, Config{Auth: clientAuth})
	if IsGate(tun.Addr().String(), 2*time.Second) {
		t.Error("a gate without HTTP taken for a proxy")
	}
}

func TestIsGateNotAnotherProgram(t *testing.T) {
	// A web server answering everything with 204, without the header.
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer web.Close()
	if IsGate(strings.TrimPrefix(web.URL, "http://"), 2*time.Second) {
		t.Error("a web server taken for the gate")
	}
	// A program that accepts and says nothing.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(io.Discard, c); c.Close() }()
		}
	}()
	start := time.Now()
	if IsGate(ln.Addr().String(), 500*time.Millisecond) {
		t.Error("a silent program taken for the gate")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the probe of a silent program took %v", d)
	}
	// Nothing listening.
	addr := ln.Addr().String()
	ln.Close()
	if IsGate(addr, 500*time.Millisecond) {
		t.Error("a closed port taken for the gate")
	}
}

// A web page cannot use the probe to reach anything: it gets the bare 204.
func TestProbeUsesNoCore(t *testing.T) {
	f := startFakeCore(t, coreAuth)
	g := newGate(t, Config{Auth: clientAuth, Open: true, HTTP: true})
	g.SetTarget(f.target())
	if code := rawHTTP(t, g, "GET "+ProbePath+" HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); code != http.StatusNoContent {
		t.Fatalf("probe answered %d", code)
	}
	if code := rawHTTP(t, g, "GET "+ProbePath+"x HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); code != http.StatusBadRequest {
		t.Errorf("another origin-form path answered %d", code)
	}
}
