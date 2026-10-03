package main

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

func TestEffectiveCaps(t *testing.T) {
	status := []byte("Name:\tcoreshiftd\nCapInh:\t0000000000000000\nCapPrm:\t0000000000001000\nCapEff:\t0000000000001000\nCapBnd:\t000001ffffffffff\n")
	caps, ok := effectiveCaps(status)
	if !ok || !hasCap(caps, capNetAdmin) {
		t.Fatalf("CAP_NET_ADMIN not found: %x %v", caps, ok)
	}
	root, _ := effectiveCaps([]byte("CapEff:\t000001ffffffffff\n"))
	if !hasCap(root, capNetAdmin) {
		t.Error("root's capabilities lack CAP_NET_ADMIN")
	}
	none, ok := effectiveCaps([]byte("CapEff:\t0000000000000000\n"))
	if !ok || hasCap(none, capNetAdmin) {
		t.Error("an unprivileged process has CAP_NET_ADMIN")
	}
	if _, ok := effectiveCaps([]byte("Name:\tx\n")); ok {
		t.Error("no CapEff line, yet parsed")
	}
	if _, ok := effectiveCaps([]byte("CapEff:\tzz\n")); ok {
		t.Error("garbage parsed")
	}
}

// fakeSession plays the app coming and going for followApp.
type fakeSession struct {
	mu          sync.Mutex
	attached    chan struct{} // receives when the app comes
	gone        chan bool     // what WaitAppGone returns
	autoConnect int
	disconnect  int
}

func (f *fakeSession) WaitAppAttached(ctx context.Context) bool {
	select {
	case <-f.attached:
		return true
	case <-ctx.Done():
		return false
	}
}

func (f *fakeSession) WaitAppGone(ctx context.Context, _, _ time.Duration) bool {
	select {
	case g := <-f.gone:
		return g
	case <-ctx.Done():
		return false
	}
}

func (f *fakeSession) AutoConnect(ctx context.Context) error {
	f.mu.Lock()
	f.autoConnect++
	f.mu.Unlock()
	<-ctx.Done() // retrying until the app goes
	return ctx.Err()
}

func (f *fakeSession) Disconnect() {
	f.mu.Lock()
	f.disconnect++
	f.mu.Unlock()
}

func (f *fakeSession) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.autoConnect, f.disconnect
}

func TestFollowApp(t *testing.T) {
	f := &fakeSession{attached: make(chan struct{}), gone: make(chan bool)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		followApp(ctx, f, io.Discard)
		close(done)
	}()
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
		}
	}
	if a, d := f.counts(); a != 0 || d != 0 {
		t.Fatalf("before the app: autoConnect %d, disconnect %d", a, d)
	}
	for round := 1; round <= 2; round++ {
		f.attached <- struct{}{}
		waitFor("no auto-connect when the app came", func() bool { a, _ := f.counts(); return a == round })
		f.gone <- true
		waitFor("no disconnect when the app went", func() bool { _, d := f.counts(); return d == round })
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("followApp did not end with its context")
	}
	if _, d := f.counts(); d != 2 {
		t.Fatalf("disconnected %d times on shutdown, want no extra", d)
	}
}

func TestCoreFile(t *testing.T) {
	if coreFile("sing-box", "windows") != "sing-box.exe" || coreFile("sing-box", "linux") != "sing-box" {
		t.Error("core file names do not match what findCores looks for")
	}
}
