package service

import (
	"runtime"
	"testing"
	"time"
)

// settledGoroutines returns the goroutine count once it is down to limit,
// or has not changed for a second: stopped cores and closed connections
// take a moment to wind down.
func settledGoroutines(limit int) int {
	n, since := runtime.NumGoroutine(), time.Now()
	for deadline := time.Now().Add(5 * time.Second); n > limit && time.Since(since) < time.Second && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		if m := runtime.NumGoroutine(); m != n {
			n, since = m, time.Now()
		}
	}
	return n
}

func goroutineDump() string {
	buf := make([]byte, 1<<20)
	return string(buf[:runtime.Stack(buf, true)])
}

// Connecting, reconnecting and disconnecting many times, with the watchers
// of each connection (traffic, network, health checks) running, leaves no
// goroutine behind: a phone keeps the engine for days.
func TestNoGoroutineLeakAcrossConnections(t *testing.T) {
	h := newHarness(t, nil)
	// One connection first: the HTTP clients' and the hub's goroutines
	// that stay for the process are not a leak.
	if err := h.connect(t, hy2Link); err != nil {
		t.Fatal(err)
	}
	h.svc.Disconnect()
	base := settledGoroutines(0)

	const cycles = 10
	for i := range cycles {
		if err := h.connect(t, hy2Link); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		// Let the health check and the traffic watcher run.
		time.Sleep(150 * time.Millisecond)
		if i%2 == 0 {
			// Switching servers replaces the connection without Disconnect.
			if err := h.connect(t, trojanLink); err != nil {
				t.Fatalf("cycle %d, switch: %v", i, err)
			}
		}
		h.svc.Disconnect()
	}
	got := settledGoroutines(base + 2)
	t.Logf("goroutines: %d after the first connection, %d after %d more", base, got, cycles)
	if got > base+2 {
		t.Fatalf("goroutines grew from %d to %d over %d connections:\n%s", base, got, cycles, goroutineDump())
	}
}

// A core that crashes and is replaced, over and over, leaves nothing either.
func TestNoGoroutineLeakAcrossCoreRestarts(t *testing.T) {
	t.Setenv("FAKECORE_XRAY", "crash-after:300ms")
	h := newHarness(t, nil)
	base := settledGoroutines(0)
	for i := range 4 {
		if err := h.connect(t, trojanLink); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		// Xray crashes; sing-box takes over.
		deadline := time.Now().Add(10 * time.Second)
		for h.svc.Status().Core != "sing-box" {
			if time.Now().After(deadline) {
				t.Fatalf("cycle %d: no swap, status %+v", i, h.svc.Status())
			}
			time.Sleep(20 * time.Millisecond)
		}
		h.svc.Disconnect()
	}
	got := settledGoroutines(base + 2)
	t.Logf("goroutines: %d at start, %d after 4 connections with a core swap each", base, got)
	if got > base+2 {
		dump := goroutineDump()
		t.Fatalf("goroutines grew from %d to %d:\n%s", base, got, dump[:min(len(dump), 20000)])
	}
}
