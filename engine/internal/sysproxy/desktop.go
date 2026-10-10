package sysproxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// The Linux desktops, here so that the logic is tested on every OS.

// Detect returns the backends of a Linux session whose environment env
// gives: KDE's in Plasma (kwriteconfig6, or 5 in Plasma 5), GNOME's
// wherever gsettings has its proxy schema (GNOME, Cinnamon, Budgie, MATE
// with GNOME's schema, the GTK programs of other desktops). has reports
// whether a program is on the PATH.
func Detect(ctx context.Context, env func(string) string, run Runner, has func(string) bool) []Backend {
	var out []Backend
	desktop := strings.ToUpper(env("XDG_CURRENT_DESKTOP") + ":" + env("XDG_SESSION_DESKTOP") + ":" + env("DESKTOP_SESSION"))
	if strings.Contains(desktop, "KDE") || strings.Contains(desktop, "PLASMA") || env("KDE_FULL_SESSION") == "true" {
		six := has("kwriteconfig6") && has("kreadconfig6")
		five := has("kwriteconfig5") && has("kreadconfig5")
		switch {
		case six && (env("KDE_SESSION_VERSION") != "5" || !five):
			out = append(out, KDE{Run: run, Version: 6})
		case five:
			out = append(out, KDE{Run: run, Version: 5})
		}
	}
	if has("gsettings") {
		if g := (GNOME{Run: run}); g.Available(ctx) {
			out = append(out, g)
		}
	}
	return out
}

// XDGAutostart is the sign-in entry of Linux desktops: a .desktop file in
// ~/.config/autostart, which GNOME, KDE, XFCE and the others start at
// sign-in.
type XDGAutostart struct {
	Path string
	// Exec is the command, `"/usr/bin/coreshiftd" sysproxy logon`.
	Exec string
}

func (a XDGAutostart) Set() error {
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o700); err != nil {
		return err
	}
	entry := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=CoreShift proxy restore\n" +
		"Name[ru]=CoreShift: восстановление прокси\n" +
		"Comment=Puts back the system proxy if CoreShift did not end cleanly\n" +
		"Exec=" + a.Exec + "\n" +
		"NoDisplay=true\n" +
		"Terminal=false\n" +
		"X-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(a.Path, []byte(entry), 0o600)
}

func (a XDGAutostart) Clear() error {
	if err := os.Remove(a.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// linuxPaths returns the journal and the autostart file of the user whose
// environment env gives: $XDG_STATE_HOME/coreshift/sysproxy.json and
// $XDG_CONFIG_HOME/autostart/coreshift-proxy-restore.desktop, under
// ~/.local/state and ~/.config by default.
func linuxPaths(env func(string) string) (journal, autostart string) {
	home := env("HOME")
	state, config := env("XDG_STATE_HOME"), env("XDG_CONFIG_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	return filepath.Join(state, "coreshift", "sysproxy.json"), filepath.Join(config, "autostart", "coreshift-proxy-restore.desktop")
}

// quoteExec quotes a program's path for a .desktop file's Exec.
func quoteExec(path string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(path) + `"`
}
