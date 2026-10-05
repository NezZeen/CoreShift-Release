package apps

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// The Linux listing's logic, here so that it is tested on every OS.

// procRealUID parses the real uid from a /proc/<pid>/status.
func procRealUID(status []byte) (int, bool) {
	sc := bufio.NewScanner(bytes.NewReader(status))
	for sc.Scan() {
		v, ok := strings.CutPrefix(sc.Text(), "Uid:")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			return 0, false
		}
		uid, err := strconv.Atoi(f[0])
		return uid, err == nil
	}
	return 0, false
}

// nobody is the overflow uid, used by sandboxes rather than people.
const nobody = 65534

// personUID reports whether a uid belongs to a person rather than to the
// system: the distributions give people uids from 1000 up. self, the
// daemon's own uid, counts too unless it is root, for development runs.
func personUID(uid, self int) bool {
	return (uid >= 1000 && uid != nobody) || (uid == self && self != 0)
}

// linuxSystemDirs hold helpers and daemons a person would not route by
// hand. Applications under /usr/lib (firefox-esr) or /opt stay listed.
var linuxSystemDirs = []string{"/usr/libexec/", "/usr/lib/systemd/", "/lib/systemd/", "/usr/sbin/", "/sbin/", "/usr/lib/xorg/"}

// linuxSessionPrograms run in every desktop session from /usr/bin: the
// session's own plumbing (message bus, sound, the compositor and the
// shell, input methods, key agents) and the shells of terminals. They are
// a person's processes but not programs anyone routes by hand; listed,
// they buried the few that matter under "dbus-daemon" and "pipewire".
var linuxSessionPrograms = map[string]bool{
	"dbus-daemon": true, "dbus-broker": true, "dbus-broker-launch": true, "dbus-launch": true,
	"pipewire": true, "pipewire-pulse": true, "wireplumber": true, "pulseaudio": true,
	"Xwayland": true, "Xorg": true, "X": true,
	"gnome-shell": true, "gnome-session-binary": true, "gnome-keyring-daemon": true,
	"kwin_x11": true, "kwin_wayland": true, "kwin_wayland_wrapper": true, "plasmashell": true, "ksmserver": true,
	"kded5": true, "kded6": true, "kglobalaccel5": true, "kglobalaccel": true, "kactivitymanagerd": true,
	"xfce4-session": true, "xfwm4": true, "xfce4-panel": true, "xfdesktop": true, "cinnamon": true, "mate-session": true,
	"ssh-agent": true, "gpg-agent": true, "systemd": true,
	"bash": true, "sh": true, "dash": true, "zsh": true, "fish": true,
}

// linuxSessionPrefixes are families of session helpers.
var linuxSessionPrefixes = []string{"ibus-", "fcitx", "at-spi", "xdg-", "gvfs", "tracker-", "gsd-", "evolution-"}

func linuxSystemPath(path string) bool {
	for _, dir := range linuxSystemDirs {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}
	name := path[strings.LastIndexByte(path, '/')+1:]
	if linuxSessionPrograms[name] {
		return true
	}
	for _, p := range linuxSessionPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
