package sysproxy

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeGSettings answers gsettings as dconf would, from a map of
// "schema key" → GVariant text; memory makes it forget writes, as
// gsettings does without the session's D-Bus.
type fakeGSettings struct {
	values map[string]string
	calls  []string
	memory bool
	// noSchema: the proxy schema is not installed.
	noSchema bool
}

func newGSettings() *fakeGSettings {
	return &fakeGSettings{values: map[string]string{
		"org.gnome.system.proxy mode":         "'auto'",
		"org.gnome.system.proxy ignore-hosts": "['localhost', '127.0.0.0/8', '::1']",
		"org.gnome.system.proxy.http host":    "''",
		"org.gnome.system.proxy.http port":    "8080",
		"org.gnome.system.proxy.https host":   "''",
		"org.gnome.system.proxy.https port":   "0",
		"org.gnome.system.proxy.socks host":   "''",
		"org.gnome.system.proxy.socks port":   "0",
	}}
}

func (f *fakeGSettings) run(_ context.Context, name string, args ...string) (string, error) {
	if name != "gsettings" {
		return "", errors.New("not found: " + name)
	}
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "list-keys":
		if f.noSchema {
			return "", errors.New("No such schema")
		}
		return "mode\nignore-hosts\n", nil
	case "get":
		v, ok := f.values[args[1]+" "+args[2]]
		if !ok {
			return "", errors.New("No such key")
		}
		return v + "\n", nil
	case "set":
		if !f.memory {
			f.values[args[1]+" "+args[2]] = args[3]
		}
		return "", nil
	}
	return "", errors.New("unknown command")
}

func TestGNOME(t *testing.T) {
	f := newGSettings()
	before := maps.Clone(f.values)
	g := GNOME{Run: f.run}
	m := &Manager{Journal: filepath.Join(t.TempDir(), "j.json"), Backends: []Backend{g}}
	if r := m.Apply(context.Background(), port); len(r.Errors) > 0 || !slices.Equal(r.Applied, []string{"gnome"}) {
		t.Fatalf("apply: %+v", r)
	}
	for key, want := range map[string]string{
		"org.gnome.system.proxy mode":       "'manual'",
		"org.gnome.system.proxy.http host":  "'127.0.0.1'",
		"org.gnome.system.proxy.http port":  "17890",
		"org.gnome.system.proxy.https port": "17890",
		"org.gnome.system.proxy.socks host": "'127.0.0.1'",
	} {
		if got := f.values[key]; got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
	if ih := f.values["org.gnome.system.proxy ignore-hosts"]; !strings.Contains(ih, "'192.168.0.0/16'") || !strings.Contains(ih, "'localhost'") {
		t.Errorf("ignore-hosts %s", ih)
	}
	// The proxy is switched on last, once its address is there.
	if last := lastSet(f.calls); last != "set org.gnome.system.proxy mode 'manual'" {
		t.Errorf("last write %q", last)
	}
	f.calls = nil
	if r := m.Restore(context.Background()); !slices.Equal(r.Restored, []string{"gnome"}) {
		t.Fatalf("restore: %+v", r)
	}
	if !maps.Equal(f.values, before) {
		t.Errorf("restored %v, want %v", f.values, before)
	}
	// And off first.
	if first := firstSet(f.calls); first != "set org.gnome.system.proxy mode 'auto'" {
		t.Errorf("first write of the restore %q", first)
	}
}

func lastSet(calls []string) string {
	for i := len(calls) - 1; i >= 0; i-- {
		if strings.HasPrefix(calls[i], "set ") {
			return calls[i]
		}
	}
	return ""
}

func firstSet(calls []string) string {
	for _, c := range calls {
		if strings.HasPrefix(c, "set ") {
			return c
		}
	}
	return ""
}

// Without the session's D-Bus gsettings forgets what it was told: that is
// a failure, not a proxy set.
func TestGNOMEWithoutSessionBus(t *testing.T) {
	f := newGSettings()
	f.memory = true
	m := &Manager{Journal: filepath.Join(t.TempDir(), "j.json"), Backends: []Backend{GNOME{Run: f.run}}}
	r := m.Apply(context.Background(), port)
	if len(r.Applied) != 0 || len(r.Errors) == 0 || !strings.Contains(r.Errors[0], "D-Bus") {
		t.Fatalf("apply: %+v", r)
	}
}

func TestGVString(t *testing.T) {
	if got := gvString(`it's \ here`); got != `'it\'s \\ here'` {
		t.Errorf("gvString: %s", got)
	}
}

// fakeKConfig is kioslaverc for kreadconfig and kwriteconfig.
type fakeKConfig struct {
	version  string
	values   map[string]string
	signaled bool
}

func (f *fakeKConfig) run(_ context.Context, name string, args ...string) (string, error) {
	if name == "dbus-send" {
		f.signaled = strings.Contains(strings.Join(args, " "), "reparseSlaveConfiguration")
		return "", nil
	}
	if args[0] != "--file" || args[1] != "kioslaverc" || args[3] != kdeGroup || args[4] != "--key" {
		return "", errors.New("bad arguments")
	}
	switch name {
	case "kreadconfig" + f.version:
		return f.values[args[5]] + "\n", nil
	case "kwriteconfig" + f.version:
		f.values[args[5]] = args[6]
		return "", nil
	}
	return "", errors.New("not found: " + name)
}

func TestKDE(t *testing.T) {
	f := &fakeKConfig{version: "6", values: map[string]string{"ProxyType": "0", "httpProxy": "http://corp 3128"}}
	before := maps.Clone(f.values)
	k := KDE{Run: f.run, Version: 6}
	m := &Manager{Journal: filepath.Join(t.TempDir(), "j.json"), Backends: []Backend{k}}
	if r := m.Apply(context.Background(), port); len(r.Errors) > 0 {
		t.Fatalf("apply: %+v", r)
	}
	if f.values["ProxyType"] != "1" || f.values["httpProxy"] != "http://127.0.0.1 17890" || f.values["socksProxy"] != "socks://127.0.0.1 17890" {
		t.Errorf("kioslaverc %v", f.values)
	}
	if !f.signaled {
		t.Error("KIO was not told to read the settings again")
	}
	m.Restore(context.Background())
	for key, want := range before {
		if f.values[key] != want {
			t.Errorf("%s = %q, want %q", key, f.values[key], want)
		}
	}
}

func TestDetect(t *testing.T) {
	g := newGSettings()
	for _, c := range []struct {
		name  string
		env   map[string]string
		progs []string
		want  []string
	}{
		{"GNOME", map[string]string{"XDG_CURRENT_DESKTOP": "ubuntu:GNOME"}, []string{"gsettings"}, []string{"gnome"}},
		{"Plasma 6", map[string]string{"XDG_CURRENT_DESKTOP": "KDE", "KDE_SESSION_VERSION": "6"}, []string{"kwriteconfig6", "kreadconfig6", "kwriteconfig5", "kreadconfig5"}, []string{"kde"}},
		{"Plasma 5", map[string]string{"XDG_CURRENT_DESKTOP": "KDE", "KDE_SESSION_VERSION": "5"}, []string{"kwriteconfig6", "kreadconfig6", "kwriteconfig5", "kreadconfig5", "gsettings"}, []string{"kde", "gnome"}},
		{"XFCE without gsettings", map[string]string{"XDG_CURRENT_DESKTOP": "XFCE"}, nil, nil},
		{"KDE without its tools", map[string]string{"XDG_CURRENT_DESKTOP": "KDE"}, nil, nil},
	} {
		got := Detect(context.Background(), func(k string) string { return c.env[k] }, g.run, func(p string) bool { return slices.Contains(c.progs, p) })
		var names []string
		for _, b := range got {
			names = append(names, b.Name())
		}
		if !slices.Equal(names, c.want) {
			t.Errorf("%s: %v, want %v", c.name, names, c.want)
		}
		if c.name == "Plasma 5" && got[0].(KDE).Version != 5 {
			t.Errorf("Plasma 5 got kwriteconfig%d", got[0].(KDE).Version)
		}
	}
	g.noSchema = true
	if got := Detect(context.Background(), func(string) string { return "" }, g.run, func(string) bool { return true }); len(got) != 0 {
		t.Errorf("without the schema: %v", got)
	}
}

func TestXDGAutostart(t *testing.T) {
	home := t.TempDir()
	journal, path := linuxPaths(envMap{"HOME": home}.get)
	if journal != filepath.Join(home, ".local", "state", "coreshift", "sysproxy.json") ||
		path != filepath.Join(home, ".config", "autostart", "coreshift-proxy-restore.desktop") {
		t.Errorf("paths %s, %s", journal, path)
	}
	a := XDGAutostart{Path: path, Exec: quoteExec("/usr/bin/coreshiftd") + " sysproxy logon"}
	if err := a.Set(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "\nExec=\"/usr/bin/coreshiftd\" sysproxy logon\n") || !strings.HasPrefix(string(b), "[Desktop Entry]\n") {
		t.Errorf("entry:\n%s", b)
	}
	if err := a.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("entry left")
	}
	if err := a.Clear(); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
	if got := quoteExec(`/opt/my "apps"/$x`); got != `"/opt/my \"apps\"/\$x"` {
		t.Errorf("quoteExec: %s", got)
	}
}

type envMap map[string]string

func (e envMap) get(k string) string { return e[k] }
