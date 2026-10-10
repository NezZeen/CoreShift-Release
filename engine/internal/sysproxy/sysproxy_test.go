package sysproxy

import (
	"context"
	"errors"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var port = netip.MustParseAddrPort("127.0.0.1:17890")

// fakeBackend keeps its settings in memory, as a registry or dconf would.
type fakeBackend struct {
	name string
	mu   sync.Mutex
	cur  Settings
	// writes counts Write calls; failWrite fails the next ones, failRead
	// every Read.
	writes    int
	failWrite int
	failRead  bool
}

func newFake(name string, cur Settings) *fakeBackend { return &fakeBackend{name: name, cur: cur} }

func (f *fakeBackend) Name() string { return f.name }

func (f *fakeBackend) Read(context.Context) (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRead {
		return nil, errors.New("read refused")
	}
	return maps.Clone(f.cur), nil
}

func (f *fakeBackend) Write(_ context.Context, s Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.failWrite > 0 {
		f.failWrite--
		// Half written, as a failure part way through leaves it.
		f.cur["server"] = s["server"]
		return errors.New("write refused")
	}
	f.cur = maps.Clone(s)
	return nil
}

func (f *fakeBackend) settings() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.cur)
}

func (*fakeBackend) Proxy(a netip.AddrPort) Settings {
	return Settings{"enabled": "1", "server": a.String()}
}

func (*fakeBackend) Direct() Settings { return Settings{"enabled": "0", "server": ""} }

func (*fakeBackend) Owns(cur, ours Settings) bool { return sameKeys(cur, ours, "enabled", "server") }

// fakeAutostart records whether the sign-in entry is set.
type fakeAutostart struct {
	set  atomic.Bool
	sets atomic.Int32
}

func (a *fakeAutostart) Set() error   { a.set.Store(true); a.sets.Add(1); return nil }
func (a *fakeAutostart) Clear() error { a.set.Store(false); return nil }

var users = Settings{"enabled": "1", "server": "proxy.corp.example:3128"}

func manager(t *testing.T, b ...Backend) (*Manager, *fakeAutostart) {
	t.Helper()
	a := &fakeAutostart{}
	return &Manager{Journal: filepath.Join(t.TempDir(), "sysproxy.json"), Backends: b, Autostart: a, Alive: func(string) bool { return false }}, a
}

func journalOf(t *testing.T, m *Manager) Journal {
	t.Helper()
	j, err := loadJournal(m.Journal)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// The settings found are journaled before anything changes, the proxy is
// set, and Restore puts back exactly what was there, removing the
// journal and the sign-in entry.
func TestApplyAndRestore(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	m, auto := manager(t, b)
	r := m.Apply(context.Background(), port)
	if len(r.Errors) > 0 || !slices.Equal(r.Applied, []string{"fake"}) || !r.Pending {
		t.Fatalf("apply: %+v", r)
	}
	if got := b.settings(); !b.Owns(got, b.Proxy(port)) {
		t.Errorf("not applied: %v", got)
	}
	j := journalOf(t, m)
	if j.Addr != port.String() || len(j.Entries) != 1 || !maps.Equal(j.Entries[0].Previous, users) {
		t.Errorf("journal %+v", j)
	}
	if !auto.set.Load() {
		t.Error("no sign-in entry while the proxy is set")
	}

	r = m.Restore(context.Background())
	if len(r.Errors) > 0 || !slices.Equal(r.Restored, []string{"fake"}) || r.Pending {
		t.Fatalf("restore: %+v", r)
	}
	if got := b.settings(); !maps.Equal(got, users) {
		t.Errorf("restored %v, want %v", got, users)
	}
	if _, err := os.Stat(m.Journal); !os.IsNotExist(err) {
		t.Errorf("journal left: %v", err)
	}
	if auto.set.Load() {
		t.Error("sign-in entry left")
	}
	// Nothing to do the second time.
	if r := m.Restore(context.Background()); len(r.Restored)+len(r.Errors) != 0 || r.Pending {
		t.Errorf("second restore: %+v", r)
	}
}

// Applying again (a reconnect, the app opened again) keeps the user's
// settings in the journal, never CoreShift's own.
func TestApplyTwiceKeepsTheUsersSettings(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	m, _ := manager(t, b)
	m.Apply(context.Background(), port)
	other := netip.MustParseAddrPort("127.0.0.1:17999")
	m.Apply(context.Background(), other)
	j := journalOf(t, m)
	if !maps.Equal(j.Entries[0].Previous, users) || j.Entries[0].Ours["server"] != other.String() {
		t.Errorf("journal %+v", j)
	}
	m.Restore(context.Background())
	if got := b.settings(); !maps.Equal(got, users) {
		t.Errorf("restored %v", got)
	}
}

// A crash: the process that set the proxy is gone; the next one (the app
// started again, the guard, the sign-in entry) restores from the journal.
func TestRestoreAfterCrash(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	m, _ := manager(t, b)
	m.Apply(context.Background(), port)
	next := &Manager{Journal: m.Journal, Backends: []Backend{b}, Alive: func(string) bool { return false }}
	if r := next.Logon(context.Background()); !slices.Equal(r.Restored, []string{"fake"}) || r.Action != "logon" {
		t.Fatalf("logon: %+v", r)
	}
	if got := b.settings(); !maps.Equal(got, users) {
		t.Errorf("restored %v", got)
	}
}

// At sign-in, a port that answers means the VPN is up again: the proxy
// stays, and the sign-in entry, which Windows removes as it runs, is set
// again.
func TestLogonLeavesALiveProxy(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	m, auto := manager(t, b)
	m.Apply(context.Background(), port)
	var asked string
	m.Alive = func(addr string) bool { asked = addr; return true }
	auto.set.Store(false)
	r := m.Logon(context.Background())
	if !r.Alive || !r.Pending || len(r.Restored) != 0 || asked != port.String() {
		t.Fatalf("logon: %+v, asked %q", r, asked)
	}
	if !b.Owns(b.settings(), b.Proxy(port)) {
		t.Error("a live proxy was taken away")
	}
	if !auto.set.Load() {
		t.Error("the sign-in entry was not set again")
	}
}

// A proxy the user changed meanwhile is theirs: Restore leaves it.
func TestRestoreLeavesTheUsersChange(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	m, _ := manager(t, b)
	m.Apply(context.Background(), port)
	mine := Settings{"enabled": "1", "server": "10.0.0.1:8080"}
	b.Write(context.Background(), mine)
	r := m.Restore(context.Background())
	if !slices.Equal(r.Changed, []string{"fake"}) || len(r.Restored) != 0 || r.Pending {
		t.Fatalf("restore: %+v", r)
	}
	if got := b.settings(); !maps.Equal(got, mine) {
		t.Errorf("the user's proxy became %v", got)
	}
}

// CoreShift's proxy left without a journal (a journal removed by hand, a
// damaged one) is not taken for the user's: Restore then turns the proxy
// off rather than pointing it at a dead port again.
func TestLeftoverProxyIsNotTheUsers(t *testing.T) {
	b := newFake("fake", (&fakeBackend{}).Proxy(port))
	m, _ := manager(t, b)
	m.Apply(context.Background(), port)
	m.Restore(context.Background())
	if got := b.settings(); !maps.Equal(got, b.Direct()) {
		t.Errorf("restored %v, want no proxy", got)
	}
}

func TestDamagedJournal(t *testing.T) {
	b := newFake("fake", (&fakeBackend{}).Proxy(port))
	m, _ := manager(t, b)
	os.WriteFile(m.Journal, []byte("{not json"), 0o600)
	r := m.Restore(context.Background())
	if len(r.Errors) == 0 {
		t.Error("a damaged journal went unreported")
	}
	if _, err := os.Stat(m.Journal + ".corrupt"); err != nil {
		t.Errorf("not moved aside: %v", err)
	}
	// Applying then treats the proxy found, CoreShift's, as no proxy.
	m.Apply(context.Background(), port)
	if j := journalOf(t, m); !maps.Equal(j.Entries[0].Previous, b.Direct()) {
		t.Errorf("journal %+v", j)
	}
}

// A write that fails part way is undone at once, and nothing is left to
// restore.
func TestApplyFailureUndoes(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	b.failWrite = 1
	m, auto := manager(t, b)
	r := m.Apply(context.Background(), port)
	if len(r.Errors) == 0 || len(r.Applied) != 0 || r.Pending {
		t.Fatalf("apply: %+v", r)
	}
	if got := b.settings(); !maps.Equal(got, users) {
		t.Errorf("left %v", got)
	}
	if _, err := os.Stat(m.Journal); !os.IsNotExist(err) || auto.set.Load() {
		t.Errorf("journal or sign-in entry left: %v, %v", err, auto.set.Load())
	}
}

// Nothing changes when the settings cannot be read: there would be
// nothing to put back.
func TestApplyWithoutReadChangesNothing(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	b.failRead = true
	m, _ := manager(t, b)
	r := m.Apply(context.Background(), port)
	if len(r.Errors) == 0 || b.writes != 0 || r.Pending {
		t.Fatalf("apply: %+v, %d writes", r, b.writes)
	}
}

// A restore that fails keeps the journal and the sign-in entry for the
// next try.
func TestRestoreFailureKeepsTheJournal(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	m, auto := manager(t, b)
	m.Apply(context.Background(), port)
	b.failWrite = 1
	if r := m.Restore(context.Background()); !r.Pending || len(r.Errors) == 0 {
		t.Fatalf("restore: %+v", r)
	}
	if !auto.set.Load() {
		t.Error("sign-in entry removed with settings still to restore")
	}
	if r := m.Restore(context.Background()); r.Pending || !slices.Equal(r.Restored, []string{"fake"}) {
		t.Fatalf("second restore: %+v", r)
	}
	if got := b.settings(); !maps.Equal(got, users) {
		t.Errorf("restored %v", got)
	}
}

// A journal from another desktop (set in KDE, restored in GNOME) is kept
// for that desktop; the others are restored.
func TestRestoreKeepsAnotherDesktopsEntry(t *testing.T) {
	gnome, kde := newFake("gnome", maps.Clone(users)), newFake("kde", maps.Clone(users))
	m, _ := manager(t, gnome, kde)
	m.Apply(context.Background(), port)
	m.Backends = []Backend{gnome}
	r := m.Restore(context.Background())
	if !r.Pending || !slices.Equal(r.Restored, []string{"gnome"}) {
		t.Fatalf("restore: %+v", r)
	}
	if j := journalOf(t, m); len(j.Entries) != 1 || j.Entries[0].Backend != "kde" {
		t.Errorf("journal %+v", j)
	}
}

func TestNoDesktop(t *testing.T) {
	m, _ := manager(t)
	r := m.Apply(context.Background(), port)
	if len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "no supported desktop") || r.Pending {
		t.Fatalf("apply: %+v", r)
	}
	if _, err := os.Stat(m.Journal); !os.IsNotExist(err) {
		t.Error("a journal without anything in it")
	}
}

// The guard restores once the port has been dead for its grace, not at
// the first miss: a reconnect closes the port for a moment.
func TestGuardRestoresADeadProxy(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	m, _ := manager(t, b)
	m.Apply(context.Background(), port)
	var alive atomic.Bool
	alive.Store(true)
	var checks atomic.Int32
	m.Alive = func(string) bool { checks.Add(1); return alive.Load() }
	done := make(chan Report, 1)
	go func() { done <- m.GuardOnce(context.Background(), 5*time.Millisecond, 60*time.Millisecond) }()
	for checks.Load() < 3 {
		time.Sleep(time.Millisecond)
	}
	if !b.Owns(b.settings(), b.Proxy(port)) {
		t.Fatal("restored while the port answered")
	}
	// A second guard sees the first and leaves.
	if r := m.GuardOnce(context.Background(), time.Millisecond, time.Millisecond); !r.Running {
		t.Errorf("second guard: %+v", r)
	}
	alive.Store(false)
	select {
	case r := <-done:
		if r.Action != "guard" || !slices.Equal(r.Restored, []string{"fake"}) {
			t.Fatalf("guard: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the guard did not restore")
	}
	if got := b.settings(); !maps.Equal(got, users) {
		t.Errorf("restored %v", got)
	}
}

// The guard goes when the app restored by itself.
func TestGuardEndsWithTheJournal(t *testing.T) {
	b := newFake("fake", maps.Clone(users))
	m, _ := manager(t, b)
	m.Apply(context.Background(), port)
	m.Alive = func(string) bool { return true }
	done := make(chan Report, 1)
	go func() { done <- m.Guard(context.Background(), 5*time.Millisecond, time.Hour) }()
	m.Restore(context.Background())
	select {
	case r := <-done:
		if r.Pending || len(r.Restored) != 0 {
			t.Errorf("guard: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the guard did not end")
	}
}
