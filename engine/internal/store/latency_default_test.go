package store

import (
	"os"
	"path/filepath"
	"testing"
)

func onPlatform(t *testing.T, goos string) {
	t.Helper()
	old := latencyGOOS
	latencyGOOS = goos
	t.Cleanup(func() { latencyGOOS = old })
}

// The desktop tests through the server by default, Android pings.
func TestLatencyTestDefaultPerPlatform(t *testing.T) {
	for goos, want := range map[string]string{"windows": LatencyProxy, "linux": LatencyProxy, "darwin": LatencyProxy, "android": LatencyPing} {
		onPlatform(t, goos)
		if got := Defaults().Cores.LatencyTest; got != want {
			t.Errorf("%s: default %q, want %q", goos, got, want)
		}
		if d := Defaults().Cores; d.LatencyVersion != latencyVersion {
			t.Errorf("%s: defaults are not of the latest version: %+v", goos, d)
		}
		// An empty value (a hand-edited file) gets the platform's default.
		s := Defaults()
		s.Cores.LatencyTest = ""
		n, err := s.normalize()
		if err != nil || n.Cores.LatencyTest != want {
			t.Errorf("%s: normalized %q, %v", goos, n.Cores.LatencyTest, err)
		}
	}
}

// Nobody could choose before the switch: a stored "ping" on the desktop
// was the old default and becomes "proxy" once; a choice made after that
// stays. Android keeps pinging.
func TestLatencyTestUpgradeOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	open := func() *Store {
		t.Helper()
		s, err := Open(path, Options{})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	write := func(file string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name, goos, file, want string
	}{
		{"desktop, ping saved by an old copy", "windows", `{"version":1,"settings":{"tun":true,"cores":{"latency_test":"ping"}}}`, LatencyProxy},
		{"desktop, linux", "linux", `{"version":1,"settings":{"tun":true,"cores":{"latency_test":"ping"}}}`, LatencyProxy},
		{"desktop, no latency setting", "windows", `{"version":1,"settings":{"tun":true}}`, LatencyProxy},
		{"desktop, proxy forced before", "windows", `{"version":1,"settings":{"tun":true,"cores":{"latency_test":"proxy"}}}`, LatencyProxy},
		{"desktop, ping chosen after the upgrade", "windows", `{"version":1,"settings":{"tun":true,"cores":{"latency_test":"ping","latency_version":1}}}`, LatencyPing},
		{"android, ping saved by an old copy", "android", `{"version":1,"settings":{"tun":true,"cores":{"latency_test":"ping"}}}`, LatencyPing},
		{"android, no latency setting", "android", `{"version":1,"settings":{"tun":true}}`, LatencyPing},
	} {
		onPlatform(t, c.goos)
		write(c.file)
		s := open()
		if got := s.Settings().Cores; got.LatencyTest != c.want || got.LatencyVersion != latencyVersion {
			t.Errorf("%s: %+v, want %s at version %d", c.name, got, c.want, latencyVersion)
		}
	}

	// The whole story on the desktop: upgraded, then the user picks ping,
	// and the next start keeps it.
	onPlatform(t, "windows")
	write(`{"version":1,"settings":{"tun":true,"cores":{"latency_test":"ping"}}}`)
	s := open()
	set := s.Settings()
	if set.Cores.LatencyTest != LatencyProxy {
		t.Fatalf("not upgraded: %+v", set.Cores)
	}
	set.Cores.LatencyTest = LatencyPing
	if _, err := s.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	if got := open().Settings().Cores.LatencyTest; got != LatencyPing {
		t.Errorf("the choice of ping did not stay: %q", got)
	}
	// And the other way round.
	set.Cores.LatencyTest = LatencyProxy
	if _, err := s.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	if got := open().Settings().Cores.LatencyTest; got != LatencyProxy {
		t.Errorf("the choice of proxy did not stay: %q", got)
	}
}
