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
}

func newLinuxHarness(t *testing.T, resolved bool, resolvConfPath string) *linuxHarness {
	t.Helper()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "j.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &linuxHarness{link: true}
	h.g = &linuxGuard{
		journal: j,
		run: func(_ context.Context, name string, args ...string) error {
			h.commands = append(h.commands, name+" "+strings.Join(args, " "))
			return nil
		},
		resolvConfPath: resolvConfPath,
		useResolved:    func() bool { return resolved },
		linkExists:     func(string) bool { return h.link },
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
