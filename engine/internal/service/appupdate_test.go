package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"coreshift/engine/internal/selfupdate"
	"coreshift/engine/internal/store"
)

// updateFake plays the release source, the installer and the user's
// sessions.
type updateFake struct {
	mu        sync.Mutex
	rel       selfupdate.Manifest
	launched  []string
	started   [][]uint32
	checks    int
	launchErr error
}

var fakeInstaller = []byte("MZ installer")

func newUpdateFake(version string, build int) *updateFake {
	sum := sha256.Sum256(fakeInstaller)
	return &updateFake{rel: selfupdate.Manifest{
		Version: version, Build: build, Installer: "coreshift-setup.exe",
		SHA256: hex.EncodeToString(sum[:]), Size: int64(len(fakeInstaller)),
	}}
}

func (f *updateFake) install(c *Config) {
	c.SelfUpdate = true
	c.updateFirstCheck = time.Millisecond
	c.updateTick = 10 * time.Millisecond
	c.checkRelease = func(context.Context, *http.Client, selfupdate.Source) (selfupdate.Release, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.checks++
		return selfupdate.Release{Manifest: f.rel}, nil
	}
	c.downloadRelease = func(_ context.Context, _ *http.Client, rel selfupdate.Release, dir string) (string, error) {
		p := filepath.Join(dir, rel.Installer)
		return p, os.WriteFile(p, fakeInstaller, 0o600)
	}
	c.launchInstaller = func(path, _ string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.launched = append(f.launched, path)
		return f.launchErr
	}
	c.appSessions = func() []uint32 { return []uint32{3} }
	c.startApp = func(s []uint32) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.started = append(f.started, s)
		return nil
	}
}

func (f *updateFake) launches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.launched)
}

func runUpdates(t *testing.T, h *harness) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.svc.RunAppUpdates(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func waitUpdate(t *testing.T, h *harness, what string, ok func(AppUpdate) bool) AppUpdate {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := h.svc.AppUpdateState()
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: never happened; state %+v", what, st)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func readPending(t *testing.T, h *harness) pendingUpdate {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.svc.updatesDir(), "pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p pendingUpdate
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAppUpdateInstallsWhileDisconnected(t *testing.T) {
	f := newUpdateFake("9.0.0", 5)
	h := newHarness(t, f.install)
	runUpdates(t, h)
	waitUpdate(t, h, "install", func(u AppUpdate) bool { return u.State == UpdateInstalling && f.launches() == 1 })
	p := readPending(t, h)
	if p.To != "9.0.0+5" || p.Reconnect || !slices.Equal(p.Sessions, []uint32{3}) {
		t.Errorf("pending = %+v", p)
	}
	// It runs from a copy in a folder of its own, not from the download.
	f.mu.Lock()
	launched := f.launched[0]
	f.mu.Unlock()
	if dir := filepath.Dir(launched); filepath.Dir(dir) != h.svc.updatesDir() || !strings.HasPrefix(filepath.Base(dir), stagePrefix) {
		t.Errorf("launched %s", launched)
	}
	// The next start removes it.
	h.svc.finishAppUpdate(context.Background())
	if _, err := os.Stat(filepath.Dir(launched)); err == nil {
		t.Error("the installer's folder stays after the update")
	}
}

func TestAppUpdateWaitsForTheVPN(t *testing.T) {
	f := newUpdateFake("9.0.0", 5)
	h := newHarness(t, f.install)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	runUpdates(t, h)
	st := waitUpdate(t, h, "download", func(u AppUpdate) bool { return u.State == UpdateReady })
	if !st.Waiting || st.Version != "9.0.0" {
		t.Errorf("state = %+v", st)
	}
	time.Sleep(50 * time.Millisecond)
	if f.launches() != 0 || h.svc.Status().State != Connected {
		t.Fatalf("installed while connected: %d launches, %+v", f.launches(), h.svc.Status())
	}

	// The user's button: the connection comes back after the update.
	if err := h.svc.InstallAppUpdate(); err != nil {
		t.Fatal(err)
	}
	if f.launches() != 1 || h.svc.Status().State != Idle {
		t.Errorf("%d launches, %+v", f.launches(), h.svc.Status())
	}
	if p := readPending(t, h); !p.Reconnect {
		t.Errorf("pending = %+v", p)
	}
}

func TestAppUpdateUpToDate(t *testing.T) {
	f := newUpdateFake("0.0.0", 0) // "dev" in tests counts as 0.0.0, build 0
	h := newHarness(t, f.install)
	runUpdates(t, h)
	waitUpdate(t, h, "check", func(u AppUpdate) bool { return u.State == UpdateIdle && !u.CheckedAt.IsZero() })
	if f.launches() != 0 {
		t.Error("installed an update that is not newer")
	}
}

func TestAppUpdateFinishesAfterRestart(t *testing.T) {
	f := newUpdateFake("0.0.0", 0)
	h := newHarness(t, f.install)
	os.MkdirAll(h.svc.updatesDir(), 0o700)
	b, _ := json.Marshal(pendingUpdate{From: "0.0.0+0", To: releaseKey(Version, BuildNumber()), Label: "dev", Sessions: []uint32{1, 2}})
	os.WriteFile(filepath.Join(h.svc.updatesDir(), "pending.json"), b, 0o600)
	runUpdates(t, h)
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		started := slices.Clone(f.started)
		f.mu.Unlock()
		if len(started) == 1 && slices.Equal(started[0], []uint32{1, 2}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("app not started again: %v", started)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(h.svc.updatesDir(), "pending.json")); err == nil {
		t.Error("pending.json left behind")
	}
}

func TestFailedUpdateIsNotRetriedByItself(t *testing.T) {
	f := newUpdateFake("9.0.0", 5)
	h := newHarness(t, f.install)
	// The installer ran, but the service that started is still this one.
	os.MkdirAll(h.svc.updatesDir(), 0o700)
	b, _ := json.Marshal(pendingUpdate{To: "9.0.0+5", Label: "9.0.0 (build 5)"})
	os.WriteFile(filepath.Join(h.svc.updatesDir(), "pending.json"), b, 0o600)
	runUpdates(t, h)
	waitUpdate(t, h, "download", func(u AppUpdate) bool { return u.State == UpdateReady })
	time.Sleep(50 * time.Millisecond)
	if f.launches() != 0 {
		t.Fatal("retried a failed update by itself")
	}
	// A later release is installed again.
	f.mu.Lock()
	f.rel.Build = 6
	f.mu.Unlock()
	if err := h.svc.CheckAppUpdate(); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, h, "install", func(u AppUpdate) bool { return u.State == UpdateInstalling && f.launches() == 1 })
}

func TestTamperedDownloadIsNotRun(t *testing.T) {
	f := newUpdateFake("9.0.0", 5)
	h := newHarness(t, func(c *Config) {
		f.install(c)
		c.downloadRelease = func(_ context.Context, _ *http.Client, rel selfupdate.Release, dir string) (string, error) {
			p := filepath.Join(dir, rel.Installer)
			return p, os.WriteFile(p, []byte("MZ something else"), 0o600)
		}
	})
	runUpdates(t, h)
	st := waitUpdate(t, h, "refusal", func(u AppUpdate) bool { return u.State == UpdateError })
	if f.launches() != 0 || !strings.Contains(st.Error, "changed") {
		t.Errorf("%d launches, state %+v", f.launches(), st)
	}
}

func TestSelfUpdateOffByDefault(t *testing.T) {
	h := newHarness(t, nil)
	if st := h.svc.AppUpdateState(); st.State != UpdateOff {
		t.Errorf("state = %+v", st)
	}
	if err := h.svc.CheckAppUpdate(); err == nil {
		t.Error("check allowed with self-update off")
	}
}

func TestUpdateTheUserInstalls(t *testing.T) {
	f := newUpdateFake("9.0.0", 5)
	var mu sync.Mutex
	var offered []string
	h := newHarness(t, func(c *Config) {
		f.install(c)
		c.InstallUpdate = func(path string) error {
			mu.Lock()
			defer mu.Unlock()
			offered = append(offered, path)
			return nil
		}
	})
	runUpdates(t, h)
	st := waitUpdate(t, h, "download", func(u AppUpdate) bool { return u.State == UpdateReady })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := len(offered)
	mu.Unlock()
	if n != 0 || st.Waiting {
		t.Fatalf("the installer was started without the user: %d, %+v", n, st)
	}

	// The user's button, while connected: the VPN stays until the update
	// replaces the app, and comes back after it.
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.InstallAppUpdate(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	n = len(offered)
	mu.Unlock()
	if n != 1 || h.svc.Status().State != Connected || h.svc.AppUpdateState().State != UpdateReady {
		t.Errorf("%d offers, %+v, %+v", n, h.svc.Status(), h.svc.AppUpdateState())
	}
	if p := readPending(t, h); !p.Reconnect {
		t.Errorf("pending = %+v", p)
	}

	// Declined: the next start neither reports a failure nor stops
	// offering it.
	h.svc.finishAppUpdate(context.Background())
	if st := h.svc.AppUpdateState(); st.State == UpdateError || h.svc.upd.failed != "" {
		t.Errorf("declined update counted as failed: %+v", st)
	}
}

// After an update that interrupted a connection, with "Автозапуск" on, the
// new service has two reasons to connect at start: the update's reconnect
// and AutoConnect. It must connect once, not connect, tear the connection
// down a second later and connect again.
func TestOneConnectAfterAnUpdate(t *testing.T) {
	for i := range 6 { // the two race: either may come first
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
			if err != nil {
				t.Fatal(err)
			}
			set := st.Settings()
			set.AutoConnect = true
			set.Cores.HealthURL, set.Cores.HealthIntervalS = "http://health.test/generate_204", 3600
			if _, err := st.SetSettings(set); err != nil {
				t.Fatal(err)
			}
			sub, err := st.Add(context.Background(), store.AddRequest{Name: "s", Content: trojanLink})
			if err != nil {
				t.Fatal(err)
			}
			n := sub.Nodes[0]
			if _, err := st.Select(sub.ID, n.Fingerprint(), n.Name); err != nil {
				t.Fatal(err)
			}
			f := newUpdateFake("0.0.0", 0)
			h := newHarness(t, func(c *Config) {
				f.install(c)
				c.Store = st
			})
			os.MkdirAll(h.svc.updatesDir(), 0o700)
			b, _ := json.Marshal(pendingUpdate{From: "0.0.0+0", To: releaseKey(Version, BuildNumber()), Label: "dev", Reconnect: true})
			os.WriteFile(filepath.Join(h.svc.updatesDir(), "pending.json"), b, 0o600)

			events, unsubscribe := h.svc.Subscribe(false)
			defer unsubscribe()
			var mu sync.Mutex
			connecting := 0
			go func() {
				for e := range events {
					if e.Kind == "state" && e.State == Connecting {
						mu.Lock()
						connecting++
						mu.Unlock()
					}
				}
			}()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			if i%2 == 0 {
				runUpdates(t, h)
				go func() { done <- h.svc.AutoConnect(ctx) }()
			} else {
				go func() { done <- h.svc.AutoConnect(ctx) }()
				runUpdates(t, h)
			}
			if err := <-done; err != nil {
				t.Fatalf("AutoConnect: %v", err)
			}
			h.waitState(t, Connected, 10*time.Second)
			time.Sleep(300 * time.Millisecond) // a second connect would be under way by now
			mu.Lock()
			got := connecting
			mu.Unlock()
			if got != 1 || h.svc.Status().State != Connected {
				t.Errorf("connected %d times, status %+v; calls %v", got, h.svc.Status(), h.log.get())
			}
		})
	}
}
