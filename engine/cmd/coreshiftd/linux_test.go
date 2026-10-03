package main

import (
	"context"
	"io"
	"strings"
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

// fakeSession plays the app coming and going for autoConnectWithApp.
type fakeSession struct {
	mu          sync.Mutex
	attached    chan struct{} // receives when the app comes
	gone        chan bool     // what WaitAppGone returns
	autoConnect int
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

func (f *fakeSession) AutoConnect(context.Context) error {
	f.mu.Lock()
	f.autoConnect++
	f.mu.Unlock()
	return nil
}

func (f *fakeSession) connects() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.autoConnect
}

// Each start of the app auto-connects, as the Windows service does when
// the app starts it; closing the app disconnects nothing (the fake has no
// Disconnect to call).
func TestAutoConnectWithApp(t *testing.T) {
	f := &fakeSession{attached: make(chan struct{}), gone: make(chan bool)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		autoConnectWithApp(ctx, f, io.Discard)
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
	time.Sleep(20 * time.Millisecond)
	if f.connects() != 0 {
		t.Fatal("auto-connect before the app started: the daemon runs from boot, not from sign-in")
	}
	for round := 1; round <= 2; round++ {
		f.attached <- struct{}{}
		waitFor("no auto-connect when the app started", func() bool { return f.connects() == round })
		f.gone <- true
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("autoConnectWithApp did not end with its context")
	}
}

func TestDetectInit(t *testing.T) {
	for _, c := range []struct {
		paths []string
		want  initSystem
	}{
		{[]string{"/run/systemd/system", "/run/openrc"}, initSystemd},
		{[]string{"/run/openrc"}, initOpenRC},
		{[]string{"/run/runit"}, initRunit},
		{[]string{"/var/service", "/usr/bin/sv"}, initRunit},
		{[]string{"/var/service"}, initUnknown},
		{nil, initUnknown},
	} {
		has := map[string]bool{}
		for _, p := range c.paths {
			has[p] = true
		}
		if got := detectInit(func(p string) bool { return has[p] }); got != c.want {
			t.Errorf("detectInit(%v) = %q, want %q", c.paths, got, c.want)
		}
	}
}

func TestServiceCommand(t *testing.T) {
	for init, want := range map[initSystem]string{
		initSystemd: "systemctl start coreshift.service",
		initOpenRC:  "rc-service coreshift start",
		initRunit:   "sv start coreshift",
	} {
		argv, err := serviceCommand(init, "start")
		if err != nil || strings.Join(argv, " ") != want {
			t.Errorf("%s: %v, %v", init, argv, err)
		}
	}
	if _, err := serviceCommand(initUnknown, "start"); err == nil {
		t.Error("no init system, yet a command")
	}
	if _, err := serviceCommand(initSystemd, "enable; rm -rf /"); err == nil {
		t.Error("unknown action accepted")
	}
}

func TestCoreFile(t *testing.T) {
	if coreFile("sing-box", "windows") != "sing-box.exe" || coreFile("sing-box", "linux") != "sing-box" {
		t.Error("core file names do not match what findCores looks for")
	}
}
