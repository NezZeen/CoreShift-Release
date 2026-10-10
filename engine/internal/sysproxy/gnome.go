package sysproxy

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// Runner runs a program and returns what it printed: exec in real use, a
// fake in tests.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// Exec runs name with exec.CommandContext.
func Exec(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return string(out), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return string(out), fmt.Errorf("%s: %w", name, err)
	}
	return string(out), nil
}

// lanHosts are kept off the proxy: loopback, the local network and the
// names of the local network.
var lanHosts = []string{"localhost", "127.0.0.0/8", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "fe80::/10", "*.local"}

// GNOME is GNOME's proxy setting (org.gnome.system.proxy), through
// gsettings: what GNOME, Cinnamon, Budgie and the GTK programs read,
// Chrome and Firefox among them. Values are GVariant text, as gsettings
// prints them, so the settings found are written back exactly.
type GNOME struct {
	Run Runner
}

const gnomeSchema = "org.gnome.system.proxy"

// gnomeKeys are the keys read, written and put back; "mode" last, so the
// proxy is switched on once its address is there, and off first of all.
var gnomeKeys = []string{"ignore-hosts", "http.host", "http.port", "https.host", "https.port", "socks.host", "socks.port", "mode"}

func (GNOME) Name() string { return "gnome" }

// schemaKey splits "http.port" into the schema and the key.
func schemaKey(k string) (string, string) {
	if sub, key, ok := strings.Cut(k, "."); ok {
		return gnomeSchema + "." + sub, key
	}
	return gnomeSchema, k
}

// Available reports whether gsettings has the proxy schema.
func (g GNOME) Available(ctx context.Context) bool {
	_, err := g.Run(ctx, "gsettings", "list-keys", gnomeSchema)
	return err == nil
}

func (g GNOME) Read(ctx context.Context) (Settings, error) {
	s := Settings{}
	for _, k := range gnomeKeys {
		schema, key := schemaKey(k)
		out, err := g.Run(ctx, "gsettings", "get", schema, key)
		if err != nil {
			return nil, err
		}
		s[k] = strings.TrimSpace(out)
	}
	return s, nil
}

func (g GNOME) Write(ctx context.Context, s Settings) error {
	// Off first: a proxy half written is not switched on meanwhile.
	keys := slices.Clone(gnomeKeys)
	if s["mode"] != "'manual'" {
		keys = append([]string{"mode"}, keys[:len(keys)-1]...)
	}
	for _, k := range keys {
		v, ok := s[k]
		if !ok || v == "" {
			continue
		}
		schema, key := schemaKey(k)
		if _, err := g.Run(ctx, "gsettings", "set", schema, key, v); err != nil {
			return err
		}
	}
	// Without the session's D-Bus gsettings saves nowhere and says so
	// only as a warning: read it back.
	schema, key := schemaKey("mode")
	out, err := g.Run(ctx, "gsettings", "get", schema, key)
	if err != nil {
		return err
	}
	if want := s["mode"]; want != "" && strings.TrimSpace(out) != want {
		return fmt.Errorf("gsettings did not keep the setting (mode is %s): no session D-Bus?", strings.TrimSpace(out))
	}
	return nil
}

func (GNOME) Proxy(addr netip.AddrPort) Settings {
	host, port := gvString(addr.Addr().String()), strconv.Itoa(int(addr.Port()))
	quoted := make([]string, len(lanHosts))
	for i, h := range lanHosts {
		quoted[i] = gvString(h)
	}
	return Settings{
		"mode":         "'manual'",
		"http.host":    host,
		"http.port":    port,
		"https.host":   host,
		"https.port":   port,
		"socks.host":   host,
		"socks.port":   port,
		"ignore-hosts": "[" + strings.Join(quoted, ", ") + "]",
	}
}

func (GNOME) Direct() Settings { return Settings{"mode": "'none'"} }

func (GNOME) Owns(cur, ours Settings) bool {
	return sameKeys(cur, ours, "mode", "http.host", "http.port")
}

// gvString is s as a GVariant string.
func gvString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// KDE is Plasma's proxy setting: the [Proxy Settings] group of
// kioslaverc, which KDE programs, and Chrome on KDE, read. Written with
// kwriteconfig5/6, then KIO is told to read it again.
type KDE struct {
	Run Runner
	// Version is 5 or 6: which kwriteconfig and kreadconfig there are.
	Version int
}

const kdeGroup = "Proxy Settings"

// kdeKeys: "ProxyType" last, as GNOME's mode.
var kdeKeys = []string{"httpProxy", "httpsProxy", "socksProxy", "NoProxyFor", "ReversedException", "ProxyType"}

func (KDE) Name() string { return "kde" }

func (k KDE) tool(what string) string { return fmt.Sprintf("k%sconfig%d", what, k.Version) }

func (k KDE) Read(ctx context.Context) (Settings, error) {
	s := Settings{}
	for _, key := range kdeKeys {
		out, err := k.Run(ctx, k.tool("read"), "--file", "kioslaverc", "--group", kdeGroup, "--key", key)
		if err != nil {
			return nil, err
		}
		s[key] = strings.TrimRight(out, "\r\n")
	}
	return s, nil
}

func (k KDE) Write(ctx context.Context, s Settings) error {
	keys := slices.Clone(kdeKeys)
	if s["ProxyType"] != "1" {
		keys = append([]string{"ProxyType"}, keys[:len(keys)-1]...)
	}
	for _, key := range keys {
		v, ok := s[key]
		if !ok {
			continue
		}
		if _, err := k.Run(ctx, k.tool("write"), "--file", "kioslaverc", "--group", kdeGroup, "--key", key, v); err != nil {
			return err
		}
	}
	// Running KDE programs read it again; without a session bus they
	// still read the file when they start.
	_, _ = k.Run(ctx, "dbus-send", "--session", "--type=signal", "/KIO/Scheduler",
		"org.kde.KIO.Scheduler.reparseSlaveConfiguration", "string:")
	return nil
}

func (KDE) Proxy(addr netip.AddrPort) Settings {
	host, port := addr.Addr().String(), strconv.Itoa(int(addr.Port()))
	return Settings{
		"ProxyType":         "1",
		"httpProxy":         "http://" + host + " " + port,
		"httpsProxy":        "http://" + host + " " + port,
		"socksProxy":        "socks://" + host + " " + port,
		"NoProxyFor":        strings.Join(lanHosts, ","),
		"ReversedException": "false",
	}
}

func (KDE) Direct() Settings { return Settings{"ProxyType": "0"} }

func (KDE) Owns(cur, ours Settings) bool { return sameKeys(cur, ours, "ProxyType", "httpProxy") }
