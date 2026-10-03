package dnsguard

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type linuxHarness struct {
	g        *linuxGuard
	commands []string
	link     bool
	nm       bool
	fw       bool
	se       bool
	stack    dnsStack
	inputs   []string
}

func newLinuxHarness(t *testing.T, resolved bool, resolvConfPath string) *linuxHarness {
	t.Helper()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "j.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &linuxHarness{link: true, stack: stackFile}
	if resolved {
		h.stack = stackResolved
	}
	dirs := slices.Clone(trustedLinkDirs)
	if resolvConfPath != "" {
		dirs = append(dirs, filepath.Dir(resolvConfPath))
	}
	h.g = &linuxGuard{
		journal: j,
		run: func(_ context.Context, name string, args ...string) error {
			h.commands = append(h.commands, name+" "+strings.Join(args, " "))
			return nil
		},
		runInput: func(_ context.Context, in []byte, name string, args ...string) error {
			h.commands = append(h.commands, name+" "+strings.Join(args, " "))
			h.inputs = append(h.inputs, string(in))
			return nil
		},
		resolvConfPath: resolvConfPath,
		detect:         func() dnsStack { return h.stack },
		firewalld:      func() bool { return h.fw },
		selinux:        func() bool { return h.se },
		networkManager: func() bool { return h.nm },
		linkExists:     func(string) bool { return h.link },
		linkDirs:       dirs,
	}
	return h
}

func TestLinuxResolved(t *testing.T) {
	h := newLinuxHarness(t, true, "")
	ctx := context.Background()
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"resolvectl dns coreshift 172.19.0.2",
		"resolvectl domain coreshift ~.",
		"resolvectl default-route coreshift yes",
		"resolvectl flush-caches",
	}
	if !slices.Equal(h.commands, want) {
		t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(h.commands, "\n"), strings.Join(want, "\n"))
	}

	h.commands = nil
	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.commands, []string{"resolvectl revert coreshift"}) {
		t.Fatalf("revert commands = %v", h.commands)
	}
}

func TestLinuxResolvedLinkAlreadyGone(t *testing.T) {
	h := newLinuxHarness(t, true, "")
	ctx := context.Background()
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	h.link = false
	h.commands = nil
	if err := h.g.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.commands) != 0 {
		t.Fatalf("no commands expected for a removed link, got %v", h.commands)
	}
	if h.g.journal.Len() != 0 {
		t.Fatal("journal should be empty")
	}
}

func TestLinuxResolvConfRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	orig := []byte("nameserver 192.168.1.1\nsearch lan\n")
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	h := newLinuxHarness(t, false, path)
	ctx := context.Background()

	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if ns := resolvConfNameservers(got); !slices.Equal(ns, tunDNS) {
		t.Fatalf("nameservers while up = %v", ns)
	}

	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != string(orig) {
		t.Fatalf("restored content = %q, want %q", got, orig)
	}
}

func TestLinuxResolvConfCreatedThenRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	h := newLinuxHarness(t, false, path)
	ctx := context.Background()
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file should be removed again, stat err = %v", err)
	}
}

// With SELinux, the resolv.conf written and the one restored are
// relabelled: the atomic write leaves them etc_t, which NetworkManager may
// not replace in enforcing mode.
func TestLinuxResolvConfRelabelledUnderSELinux(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte("nameserver 192.168.1.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newLinuxHarness(t, false, path)
	h.se = true
	ctx := context.Background()
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	want := []string{"restorecon " + path}
	if !slices.Equal(h.commands, want) {
		t.Fatalf("apply commands = %v, want %v", h.commands, want)
	}
	h.commands = nil
	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.commands, want) {
		t.Fatalf("revert commands = %v, want %v", h.commands, want)
	}

	// Without SELinux, nothing is run; nor for a file that was not there.
	h.se = false
	h.commands = nil
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.commands) != 0 {
		t.Fatalf("commands without SELinux = %v", h.commands)
	}
	missing := filepath.Join(t.TempDir(), "resolv.conf")
	h2 := newLinuxHarness(t, false, missing)
	h2.se = true
	if err := h2.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	h2.commands = nil
	if err := h2.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h2.commands) != 0 {
		t.Fatalf("revert of a created file ran %v", h2.commands)
	}
}

func TestLinuxResolvConfReplacedByOthersIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	os.WriteFile(path, []byte("nameserver 192.168.1.1\n"), 0o644)
	h := newLinuxHarness(t, false, path)
	ctx := context.Background()
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	theirs := []byte("# NetworkManager\nnameserver 10.0.0.1\n")
	os.WriteFile(path, theirs, 0o644)
	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(theirs) {
		t.Fatalf("someone else's resolv.conf was clobbered: %q", got)
	}
}

func TestLinuxResolvConfSymlinkRestored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "stub-resolv.conf")
	os.WriteFile(target, []byte("nameserver 127.0.0.53\n"), 0o644)
	path := filepath.Join(dir, "resolv.conf")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	h := newLinuxHarness(t, false, path)
	ctx := context.Background()
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "nameserver 127.0.0.53\n" {
		t.Fatal("symlink target must not be modified")
	}
	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(path); err != nil || link != target {
		t.Fatalf("symlink not restored: %q, %v", link, err)
	}
}

func TestLinuxNetworkManagerLeavesTheLinkAlone(t *testing.T) {
	h := newLinuxHarness(t, true, "")
	h.nm = true
	if err := h.g.Apply(context.Background(), Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	if len(h.commands) == 0 || h.commands[0] != "nmcli device set coreshift managed no" {
		t.Fatalf("commands = %v, want nmcli first", h.commands)
	}
}

// NetworkManager or a DHCP client replaces resolv.conf while the tunnel is
// up: Keep puts ours back, and what they wrote is restored at the end.
func TestLinuxResolvConfKeptAgainstRewrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	os.WriteFile(path, []byte("nameserver 192.168.1.1\n"), 0o644)
	h := newLinuxHarness(t, false, path)
	ctx := context.Background()
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	if err := h.g.Keep(ctx); err != nil {
		t.Fatal(err)
	}
	if h.g.journal.Len() != 1 {
		t.Fatalf("journal has %d changes, want 1", h.g.journal.Len())
	}
	renewed := []byte("# Generated by NetworkManager\nnameserver 192.168.1.254\n")
	os.WriteFile(path, renewed, 0o644)
	if err := h.g.Keep(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if ns := resolvConfNameservers(got); !slices.Equal(ns, tunDNS) {
		t.Fatalf("nameservers after Keep = %v, want ours", ns)
	}
	if h.g.journal.Len() != 1 {
		t.Fatalf("journal has %d changes after Keep, want 1", h.g.journal.Len())
	}
	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(renewed) {
		t.Fatalf("restored %q, want the renewed %q", got, renewed)
	}
	// Nothing applied: Keep must not write.
	if err := h.g.Keep(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(renewed) {
		t.Fatalf("Keep wrote after Revert: %q", got)
	}
}

func TestLinuxKeepIgnoresResolved(t *testing.T) {
	h := newLinuxHarness(t, true, "")
	ctx := context.Background()
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	h.commands = nil
	if err := h.g.Keep(ctx); err != nil || len(h.commands) != 0 {
		t.Fatalf("Keep with resolved: err %v, commands %v", err, h.commands)
	}
}

// A forged journal must not make the daemon, which runs as root, write any
// file but resolv.conf or run resolvectl with odd arguments.
func TestLinuxRecoverRejectsForgedJournal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resolv.conf")
	victim := filepath.Join(dir, "shadow")
	os.WriteFile(victim, []byte("root:secret\n"), 0o600)
	written := renderResolvConf(Config{Servers: tunDNS})

	for name, entry := range map[string]struct {
		kind string
		data any
	}{
		"other file":      {kindResolvConf, resolvConfChange{Path: victim, Existed: true, Content: []byte("owned\n"), Mode: 0o777, Written: []byte("root:secret\n")}},
		"unclean path":    {kindResolvConf, resolvConfChange{Path: dir + "/x/../resolv.conf", Existed: true, Content: []byte("x\n")}},
		"empty path":      {kindResolvConf, resolvConfChange{Existed: true, Content: []byte("x\n")}},
		"link to shadow":  {kindResolvConf, resolvConfChange{Path: path, Existed: true, Symlink: victim, Written: written}},
		"link outside":    {kindResolvConf, resolvConfChange{Path: path, Existed: true, Symlink: "/home/user/resolv.conf", Written: written}},
		"huge":            {kindResolvConf, resolvConfChange{Path: path, Existed: true, Content: make([]byte, maxResolvConf+1), Written: written}},
		"option as iface": {kindResolvedLink, resolvedLinkChange{Interface: "--help"}},
		"long iface":      {kindResolvedLink, resolvedLinkChange{Interface: "abcdefghijklmnopq"}},
		"slash iface":     {kindResolvedLink, resolvedLinkChange{Interface: "../x"}},
		"not json":        {kindResolvConf, "just a string"},
	} {
		t.Run(name, func(t *testing.T) {
			os.WriteFile(path, written, 0o644)
			h := newLinuxHarness(t, false, path)
			if err := h.g.journal.Record(entry.kind, entry.data); err != nil {
				t.Fatal(err)
			}
			err := h.g.Recover(context.Background())
			if !errors.Is(err, ErrRejected) {
				t.Fatalf("Recover err = %v, want ErrRejected", err)
			}
			if len(h.commands) != 0 {
				t.Fatalf("ran %v", h.commands)
			}
			if b, _ := os.ReadFile(victim); string(b) != "root:secret\n" {
				t.Fatalf("victim file changed: %q", b)
			}
			if b, _ := os.ReadFile(path); string(b) != string(written) {
				t.Fatalf("resolv.conf changed: %q", b)
			}
			// Dropped, so the next connection is not blocked by it.
			if h.g.journal.Len() != 0 {
				t.Fatal("rejected entry kept in the journal")
			}
			if err := h.g.Recover(context.Background()); err != nil {
				t.Fatalf("second Recover: %v", err)
			}
		})
	}
}

func TestLinuxRecoverAcceptsOwnEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	written := renderResolvConf(Config{Servers: tunDNS})
	os.WriteFile(path, written, 0o644)
	h := newLinuxHarness(t, false, path)
	h.g.journal.Record(kindResolvedLink, resolvedLinkChange{Interface: "coreshift"})
	h.g.journal.Record(kindResolvConf, resolvConfChange{Path: path, Existed: true, Content: []byte("nameserver 10.0.0.1\n"), Mode: 0o644, Written: written})
	if err := h.g.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "nameserver 10.0.0.1\n" {
		t.Fatalf("restored %q", b)
	}
	if !slices.Equal(h.commands, []string{"resolvectl revert coreshift"}) {
		t.Fatalf("commands = %v", h.commands)
	}
}

func TestLinuxApplyLeavesUntrustedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	dir := t.TempDir()
	other := t.TempDir()
	target := filepath.Join(other, "resolv.conf")
	os.WriteFile(target, []byte("nameserver 10.0.0.1\n"), 0o644)
	path := filepath.Join(dir, "resolv.conf")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	h := newLinuxHarness(t, false, path)
	// Left alone, without failing the connection: the TUN layer hijacks
	// DNS anyway.
	if err := h.g.Apply(context.Background(), Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	if h.g.journal.Len() != 0 {
		t.Fatal("a change recorded for a link left alone")
	}
	if link, err := os.Readlink(path); err != nil || link != target {
		t.Fatalf("link changed: %q, %v", link, err)
	}
}

func TestTrustedLink(t *testing.T) {
	g := &linuxGuard{linkDirs: trustedLinkDirs}
	for target, want := range map[string]bool{
		"../run/systemd/resolve/stub-resolv.conf":  true,
		"/run/systemd/resolve/stub-resolv.conf":    true,
		"/run/NetworkManager/resolv.conf":          true,
		"/var/run/NetworkManager/resolv.conf":      true,
		"/etc/resolvconf/run/resolv.conf":          true,
		"/usr/lib/systemd/resolv.conf":             true,
		"../run/resolvconf/resolv.conf":            true,
		"/etc/shadow":                              false,
		"/run/../etc/shadow":                       false,
		"/run/user/1000/evil":                      false,
		"/home/user/resolv.conf":                   false,
		"../home/user/resolv.conf":                 false,
		"/run/systemd/resolve/../../../tmp/resolv": false,
		"/runner/resolv.conf":                      false,
		"/mnt/wsl/resolv.conf":                     true,
		"/mnt/c/resolv.conf":                       false,
	} {
		if got := g.trustedLink("/etc/resolv.conf", target); got != want {
			t.Errorf("trustedLink(%q) = %v, want %v", target, got, want)
		}
	}
}

func TestValidInterfaceName(t *testing.T) {
	for name, want := range map[string]bool{
		"coreshift": true, "tun0": true, "wg-home": true, "eth0.100": true,
		"": false, "-x": false, "--help": false, "a b": false, "a/b": false, "abcdefghijklmnop": false,
	} {
		if got := validInterfaceName(name); got != want {
			t.Errorf("validInterfaceName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestRestoredModeNeverWritableByOthers(t *testing.T) {
	for in, want := range map[os.FileMode]os.FileMode{0o644: 0o644, 0o600: 0o600, 0o777: 0o644, 0o4755: 0o644, 0: 0o644, 0o666: 0o644} {
		if got := restoredMode(in); got != want {
			t.Errorf("restoredMode(%o) = %o, want %o", in, got, want)
		}
	}
}

func TestResolvConfParsing(t *testing.T) {
	conf := []byte("# comment\nnameserver 127.0.0.53\nnameserver fe80::1%eth0\nnameserver 1.1.1.1\noptions edns0\nnameserver bogus\n")
	if !usesResolvedStub(conf) {
		t.Error("stub not detected")
	}
	if usesResolvedStub([]byte("nameserver 8.8.8.8\n")) {
		t.Error("false positive stub detection")
	}
	got := filterUsable(resolvConfNameservers(conf))
	if want := []netip.Addr{netip.MustParseAddr("1.1.1.1")}; !slices.Equal(got, want) {
		t.Errorf("usable = %v, want %v", got, want)
	}
}

func TestUsableResolver(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.1.1":        true,
		"2001:4860:4860::88": true,
		"127.0.0.1":          false,
		"0.0.0.0":            false,
		"fe80::1":            false,
		"fec0:0:0:ffff::1":   false,
	} {
		if got := usableResolver(netip.MustParseAddr(addr)); got != want {
			t.Errorf("usableResolver(%s) = %v, want %v", addr, got, want)
		}
	}
}
