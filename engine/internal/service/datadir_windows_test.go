package service

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

// fakeSecurity treats the paths in foreign as another user's and records
// what is locked and taken back.
type fakeSecurity struct {
	mu        sync.Mutex
	foreign   map[string]bool
	locked    []string
	reclaimed []string
}

func (f *fakeSecurity) trusted(path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.foreign[filepath.Clean(path)], nil
}

func (f *fakeSecurity) lock(dir string) error {
	f.locked = append(f.locked, dir)
	return nil
}

func (f *fakeSecurity) reclaim(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reclaimed = append(f.reclaimed, path)
	delete(f.foreign, filepath.Clean(path))
	return nil
}

func (f *fakeSecurity) isLink(fi fs.FileInfo) bool { return winDirSecurity{}.isLink(fi) }

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// junction makes link a directory junction to target; users can make those
// without any privilege, in a folder they may write to.
func junction(t *testing.T, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
}

func TestSecureDataDirTakesBackWhatUsersMade(t *testing.T) {
	root := filepath.Join(t.TempDir(), "CoreShift")
	outside := t.TempDir()
	write(t, filepath.Join(outside, "victim.txt"), "keep me")

	// What the service made itself.
	write(t, filepath.Join(root, "work", "xray", "config.json"), "{}")
	write(t, filepath.Join(root, "dnsguard.json"), "[]")
	// What a user planted: an installer in a folder they created, a rule
	// set in the service's own folder, a store they own, and links.
	write(t, filepath.Join(root, "updates", "installers", "setup.exe"), "evil")
	write(t, filepath.Join(root, "rules", "geoip-ru.srs"), "SRS evil")
	write(t, filepath.Join(root, "state", "store.json"), `{"version":1}`)
	junction(t, filepath.Join(root, "tun"), outside)
	junction(t, filepath.Join(root, "linked"), outside)

	sec := &fakeSecurity{foreign: map[string]bool{
		filepath.Join(root, "updates"):                    true,
		filepath.Join(root, "rules", "geoip-ru.srs"):      true,
		filepath.Join(root, "state"):                      true,
		filepath.Join(root, "state", "store.json"):        true,
		filepath.Join(root, "updates", "installers"):      true,
		filepath.Join(root, "updates", "installers", "x"): true,
	}}
	notes, err := secureDataDir(root, sec)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("notes:\n%s", strings.Join(notes, "\n"))
	if len(sec.locked) != 1 || sec.locked[0] != root {
		t.Errorf("locked %v", sec.locked)
	}
	for _, gone := range []string{"updates", "rules", "tun", "linked"} {
		if _, err := os.Lstat(filepath.Join(root, gone)); err == nil {
			t.Errorf("%s is still there", gone)
		}
	}
	for _, kept := range []string{"work/xray/config.json", "dnsguard.json", "state/store.json"} {
		if _, err := os.Stat(filepath.Join(root, kept)); err != nil {
			t.Errorf("%s: %v", kept, err)
		}
	}
	// The links went, not what they pointed to.
	if b, err := os.ReadFile(filepath.Join(outside, "victim.txt")); err != nil || string(b) != "keep me" {
		t.Errorf("the target of a link was touched: %q, %v", b, err)
	}
	// Kept folders are taken back, each before its content; removed ones are
	// taken back first too, so that they can be listed and deleted.
	for _, pair := range [][2]string{{"state", `state\store.json`}, {"updates", `updates\installers`}} {
		a := slices.Index(sec.reclaimed, filepath.Join(root, pair[0]))
		b := slices.Index(sec.reclaimed, filepath.Join(root, pair[1]))
		if a < 0 || b < 0 || a > b {
			t.Errorf("reclaimed %v: want %s, then %s", sec.reclaimed, pair[0], pair[1])
		}
	}
	if slices.Contains(sec.reclaimed, filepath.Join(root, "work")) || slices.Contains(sec.reclaimed, filepath.Join(root, "dnsguard.json")) {
		t.Errorf("took back what the service owns: %v", sec.reclaimed)
	}

	// Again: nothing left to do.
	sec.locked = nil
	if notes, err := secureDataDir(root, sec); err != nil || len(notes) != 0 {
		t.Errorf("second run: %v %v", notes, err)
	}
}

// A folder whose owner cannot even be read, closed to SYSTEM by its owner,
// is taken back as well.
func TestSecureDataDirTakesBackWhatItCannotRead(t *testing.T) {
	root := filepath.Join(t.TempDir(), "CoreShift")
	write(t, filepath.Join(root, "state", "store.json"), "{}")
	sec := &closedSecurity{fakeSecurity: fakeSecurity{foreign: map[string]bool{}}, closed: filepath.Join(root, "state")}
	if _, err := secureDataDir(root, sec); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sec.reclaimed, filepath.Join(root, "state")) {
		t.Errorf("reclaimed %v", sec.reclaimed)
	}
}

type closedSecurity struct {
	fakeSecurity
	closed string
}

func (c *closedSecurity) trusted(path string) (bool, error) {
	if path == c.closed && !slices.Contains(c.reclaimed, path) {
		return false, os.ErrPermission
	}
	return c.fakeSecurity.trusted(path)
}

func TestSecureDataDirReplacesALinkedRoot(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "victim.txt"), "keep me")
	root := filepath.Join(base, "CoreShift")
	junction(t, root, outside)
	if _, err := secureDataDir(root, &fakeSecurity{foreign: map[string]bool{}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(root)
	if err != nil || (winDirSecurity{}).isLink(fi) || !fi.IsDir() {
		t.Errorf("root is not a plain directory: %v %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "victim.txt")); err != nil {
		t.Error(err)
	}
}

// The real permissions, on a folder of the current user: the protected
// DACL of the data directory, and a reclaimed entry inheriting it.
func TestDataDirPermissions(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	me := user.User.Sid.String()
	// The owner may always change permissions: give the folders back to
	// the test's user before they are deleted.
	t.Cleanup(func() {
		for _, p := range []string{root, child} {
			setDACL(p, "D:(A;OICI;FA;;;"+me+")")
		}
	})

	if owner, err := ownerOf(root); err != nil || owner.String() == "" {
		t.Fatalf("owner: %v %v", owner, err)
	}
	if err := setDACL(child, "D:P(A;;FA;;;WD)"); err != nil {
		t.Fatal(err)
	}
	if err := setDACL(root, sddlRoot); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	ctl, _, _ := sd.Control()
	got := sd.String()
	if ctl&windows.SE_DACL_PROTECTED == 0 || !strings.Contains(got, ";;;SY)") || !strings.Contains(got, ";;;BA)") ||
		!strings.Contains(got, "(A;;0x1200a9;;;BU)") || strings.Contains(got, "WD") || strings.Count(got, "(A;") != 3 {
		t.Errorf("root DACL %s", got)
	}

	// A protected child keeps its own DACL until it is taken back.
	if err := inheritDACL(child); err != nil {
		t.Fatal(err)
	}
	sd, err = windows.GetNamedSecurityInfo(child, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	ctl, _, _ = sd.Control()
	got = sd.String()
	if ctl&windows.SE_DACL_PROTECTED != 0 || strings.Contains(got, "WD") || strings.Contains(got, "BU") ||
		!strings.Contains(got, "ID;FA;;;SY)") || !strings.Contains(got, "ID;FA;;;BA)") {
		t.Errorf("child DACL %s, want only what the root passes on", got)
	}
}

func TestWriteSharedReplacesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.json")
	// A file planted beforehand is replaced, not written through.
	other := filepath.Join(dir, "other.json")
	write(t, other, "theirs")
	if err := os.Link(other, path); err != nil {
		t.Skip(err)
	}
	if err := WriteShared(path, []byte(`{"token":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != `{"token":"x"}` {
		t.Errorf("api.json = %q", b)
	}
	if b, _ := os.ReadFile(other); string(b) != "theirs" {
		t.Errorf("the planted file's other name was written through: %q", b)
	}
}
