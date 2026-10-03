package apps

import "testing"

func TestProcRealUID(t *testing.T) {
	uid, ok := procRealUID([]byte("Name:\tfirefox\nUmask:\t0022\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\n"))
	if !ok || uid != 1000 {
		t.Fatalf("uid = %d, %v", uid, ok)
	}
	if _, ok := procRealUID([]byte("Name:\tx\n")); ok {
		t.Error("no Uid line, yet parsed")
	}
}

func TestPersonUID(t *testing.T) {
	for _, c := range []struct {
		uid, self int
		want      bool
	}{
		{1000, 0, true}, {1001, 0, true}, {0, 0, false}, {101, 0, false}, {65534, 0, false},
		{999, 999, true}, // a development run as a system user
		{0, 1000, false},
	} {
		if got := personUID(c.uid, c.self); got != c.want {
			t.Errorf("personUID(%d, self %d) = %v, want %v", c.uid, c.self, got, c.want)
		}
	}
}

func TestLinuxSystemPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/usr/libexec/gvfsd":                            true,
		"/usr/lib/systemd/systemd":                      true,
		"/usr/sbin/NetworkManager":                      true,
		"/usr/lib/firefox-esr/firefox-esr":              false,
		"/snap/firefox/5000/usr/lib/firefox/firefox":    false,
		"/usr/bin/telegram-desktop":                     false,
		"/opt/google/chrome/chrome":                     false,
		"/home/me/.local/share/Steam/ubuntu12_32/steam": false,
	} {
		if got := linuxSystemPath(path); got != want {
			t.Errorf("linuxSystemPath(%q) = %v, want %v", path, got, want)
		}
	}
}
