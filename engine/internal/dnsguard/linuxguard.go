package dnsguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// This file holds the Linux guard's logic. Commands and the resolv.conf path
// are injected so it can be tested on any OS; guard_linux.go wires it up.
//
// Three setups are handled:
//   - systemd-resolved with /etc/resolv.conf pointing at its stub (Ubuntu,
//     Fedora, and NetworkManager on top of either): the TUN link gets our
//     servers and the "~." routing domain, so every query goes there.
//   - NetworkManager writing /etc/resolv.conf itself (Debian without
//     resolved, Arch, older Fedora): we write the file, and Keep writes it
//     again whenever NetworkManager replaces it, say after a DHCP renewal.
//   - plain /etc/resolv.conf (resolvconf, dhclient, a hand-written file):
//     the same as NetworkManager.
//
// Whatever the setup, the TUN layer also hijacks every port-53 packet
// routed into the tunnel, so a resolver the guard missed still cannot leak.

const (
	kindResolvedLink = "linux.resolved"
	kindResolvConf   = "linux.resolvconf"
)

// maxResolvConf bounds a resolv.conf the journal may restore.
const maxResolvConf = 64 << 10

// trustedLinkDirs are where a symlinked /etc/resolv.conf may point: the
// files systemd-resolved, NetworkManager and resolvconf manage. A link
// anywhere else is neither replaced nor restored.
var trustedLinkDirs = []string{"/run/", "/var/run/", "/etc/resolvconf/", "/usr/lib/systemd/", "/lib/systemd/"}

type commandRunner func(ctx context.Context, name string, args ...string) error

type resolvedLinkChange struct {
	Interface string `json:"interface"`
}

type resolvConfChange struct {
	Path    string      `json:"path"`
	Existed bool        `json:"existed"`
	Symlink string      `json:"symlink,omitempty"`
	Content []byte      `json:"content,omitempty"`
	Mode    fs.FileMode `json:"mode,omitempty"`
	// Written is what we put there; if the file differs at undo time someone
	// else (NetworkManager, the user) owns it now and we leave it alone.
	Written []byte `json:"written"`
}

type linuxGuard struct {
	journal        *Journal
	run            commandRunner
	resolvConfPath string
	// useResolved reports whether systemd-resolved serves the system's DNS.
	useResolved func() bool
	// networkManager reports whether NetworkManager runs: it is told to
	// leave the TUN link alone.
	networkManager func() bool
	linkExists     func(name string) bool
	// linkDirs are the trusted symlink targets' directories (trustedLinkDirs).
	linkDirs []string

	mu sync.Mutex
	// written is the config whose resolv.conf is in place, for Keep; nil
	// when resolved is used or nothing is applied.
	written *Config
}

func (g *linuxGuard) Apply(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if !validInterfaceName(cfg.Interface) {
		return fmt.Errorf("dnsguard: invalid interface name %q", cfg.Interface)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.revertLocked(ctx); err != nil {
		return fmt.Errorf("dnsguard: revert previous state: %w", err)
	}
	if g.networkManager != nil && g.networkManager() {
		// Left managed, NetworkManager may give the link settings of its
		// own; the setting goes away with the link. Best effort.
		_ = g.run(ctx, "nmcli", "device", "set", cfg.Interface, "managed", "no")
	}
	var err error
	if g.useResolved() {
		err = g.applyResolved(ctx, cfg)
	} else if err = g.applyResolvConf(cfg); err == nil {
		c := cfg
		g.written = &c
	}
	if err != nil {
		return errors.Join(err, g.revertLocked(ctx))
	}
	return nil
}

func (g *linuxGuard) Revert(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.revertLocked(ctx)
}

func (g *linuxGuard) revertLocked(ctx context.Context) error {
	g.written = nil
	if g.journal.Len() == 0 {
		return nil
	}
	return g.journal.Undo(func(c Change) error { return g.undo(ctx, c) })
}

func (g *linuxGuard) Recover(ctx context.Context) error { return g.Revert(ctx) }

// Keep writes our resolv.conf again when someone replaced it while the
// tunnel is up: NetworkManager or a DHCP client does so on every lease
// renewal. What they wrote becomes the original restored on disconnect, as
// it is the more current one. It does nothing in the resolved setup.
func (g *linuxGuard) Keep(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.written == nil {
		return nil
	}
	cfg := *g.written
	cur, err := os.ReadFile(g.resolvConfPath)
	if err == nil && bytes.Equal(cur, renderResolvConf(cfg)) {
		return nil
	}
	// The file is someone else's now: undoing leaves it be and only clears
	// the journal, and applying again takes it as the original.
	if err := g.revertLocked(ctx); err != nil {
		return err
	}
	if err := g.applyResolvConf(cfg); err != nil {
		return errors.Join(err, g.revertLocked(ctx))
	}
	g.written = &cfg
	return nil
}

// applyResolved gives the TUN link our servers and the "~." routing domain,
// so resolved sends every query there.
func (g *linuxGuard) applyResolved(ctx context.Context, cfg Config) error {
	if err := g.journal.Record(kindResolvedLink, resolvedLinkChange{Interface: cfg.Interface}); err != nil {
		return err
	}
	servers := strings.Fields(joinAddrs(cfg.Servers, " "))
	for _, args := range [][]string{
		append([]string{"dns", cfg.Interface}, servers...),
		{"domain", cfg.Interface, "~."},
		{"default-route", cfg.Interface, "yes"},
	} {
		if err := g.run(ctx, "resolvectl", args...); err != nil {
			return fmt.Errorf("dnsguard: resolvectl %s: %w", strings.Join(args, " "), err)
		}
	}
	_ = g.run(ctx, "resolvectl", "flush-caches") // best effort; stale entries expire anyway
	return nil
}

func (g *linuxGuard) applyResolvConf(cfg Config) error {
	ch, err := snapshotFile(g.resolvConfPath)
	if err != nil {
		return fmt.Errorf("dnsguard: read %s: %w", g.resolvConfPath, err)
	}
	if ch.Symlink != "" && !g.trustedLink(ch.Path, ch.Symlink) {
		// Restoring an unusual link would have to be trusted later; better
		// to touch nothing. The TUN layer still hijacks port 53.
		return fmt.Errorf("dnsguard: %s links to %s, which CoreShift does not manage", ch.Path, ch.Symlink)
	}
	if len(ch.Content) > maxResolvConf {
		return fmt.Errorf("dnsguard: %s is larger than %d KB", ch.Path, maxResolvConf>>10)
	}
	ch.Written = renderResolvConf(cfg)
	if err := g.journal.Record(kindResolvConf, ch); err != nil {
		return err
	}
	if ch.Symlink != "" {
		// Replace the link itself, not the file it points to.
		if err := os.Remove(ch.Path); err != nil {
			return fmt.Errorf("dnsguard: replace %s: %w", ch.Path, err)
		}
	}
	if err := writeFileAtomic(ch.Path, ch.Written, 0o644); err != nil {
		return fmt.Errorf("dnsguard: write %s: %w", ch.Path, err)
	}
	return nil
}

func (g *linuxGuard) undo(ctx context.Context, c Change) error {
	switch c.Kind {
	case kindResolvedLink:
		var ch resolvedLinkChange
		if err := json.Unmarshal(c.Data, &ch); err != nil {
			return fmt.Errorf("%w: %v", ErrRejected, err)
		}
		if !validInterfaceName(ch.Interface) {
			return fmt.Errorf("%w: interface name %q", ErrRejected, ch.Interface)
		}
		// resolved drops per-link settings together with the link.
		if !g.linkExists(ch.Interface) {
			return nil
		}
		return g.run(ctx, "resolvectl", "revert", ch.Interface)
	case kindResolvConf:
		var ch resolvConfChange
		if err := json.Unmarshal(c.Data, &ch); err != nil {
			return fmt.Errorf("%w: %v", ErrRejected, err)
		}
		if err := g.checkResolvConfChange(ch); err != nil {
			return err
		}
		return restoreResolvConf(ch)
	}
	// Unknown kinds come from a newer build; this one cannot undo them.
	return nil
}

// checkResolvConfChange accepts only what applyResolvConf records: the
// guard's own resolv.conf, a link into a directory a resolver manages, and
// contents of a sane size. A journal is root's file, but should it ever be
// forged, the daemon must not write anywhere else.
func (g *linuxGuard) checkResolvConfChange(ch resolvConfChange) error {
	if ch.Path == "" || filepath.Clean(ch.Path) != filepath.Clean(g.resolvConfPath) || ch.Path != filepath.Clean(ch.Path) {
		return fmt.Errorf("%w: path %q is not %s", ErrRejected, ch.Path, g.resolvConfPath)
	}
	if ch.Symlink != "" && !g.trustedLink(ch.Path, ch.Symlink) {
		return fmt.Errorf("%w: link target %q", ErrRejected, ch.Symlink)
	}
	if len(ch.Content) > maxResolvConf || len(ch.Written) > maxResolvConf {
		return fmt.Errorf("%w: contents larger than %d KB", ErrRejected, maxResolvConf>>10)
	}
	return nil
}

// trustedLink reports whether a resolv.conf at file may link to target: a
// file named like a resolv.conf in one of linkDirs.
func (g *linuxGuard) trustedLink(file, target string) bool {
	t := filepath.ToSlash(target)
	if strings.ContainsRune(t, 0) {
		return false
	}
	if !path.IsAbs(t) && !filepath.IsAbs(target) {
		t = path.Join(path.Dir(filepath.ToSlash(file)), t)
	}
	t = path.Clean(t)
	if !strings.Contains(path.Base(t), "resolv") {
		return false
	}
	for _, d := range g.linkDirs {
		if strings.HasPrefix(t, strings.TrimSuffix(filepath.ToSlash(d), "/")+"/") {
			return true
		}
	}
	return false
}

// ifName is what the guard accepts as a Linux interface name: what the
// kernel allows (at most 15 bytes, no slash or space), minus a leading
// dash, so a name is never taken for a command's option.
var ifName = regexp.MustCompile(`^[A-Za-z0-9_.@][A-Za-z0-9_.@:-]{0,14}$`)

func validInterfaceName(name string) bool { return ifName.MatchString(name) }

func renderResolvConf(cfg Config) []byte {
	var b bytes.Buffer
	b.WriteString("# Generated by CoreShift while the tunnel is up; the original is restored on disconnect.\n")
	for _, s := range cfg.Servers {
		fmt.Fprintf(&b, "nameserver %s\n", s)
	}
	return b.Bytes()
}

func snapshotFile(path string) (resolvConfChange, error) {
	ch := resolvConfChange{Path: path}
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ch, nil
	}
	if err != nil {
		return ch, err
	}
	ch.Existed = true
	if fi.Mode()&fs.ModeSymlink != 0 {
		ch.Symlink, err = os.Readlink(path)
		return ch, err
	}
	ch.Mode = fi.Mode().Perm()
	ch.Content, err = os.ReadFile(path)
	return ch, err
}

// restoredMode is the permission a restored resolv.conf gets: what it had,
// but never writable by others or executable.
func restoredMode(m fs.FileMode) fs.FileMode {
	if m = m.Perm() & 0o644; m == 0 {
		return 0o644
	}
	return m
}

func restoreResolvConf(ch resolvConfChange) error {
	cur, err := os.ReadFile(ch.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Gone; put the original back.
	case err != nil:
		return err
	case !bytes.Equal(cur, ch.Written):
		// Either we crashed before writing it, or someone else replaced it since.
		return nil
	}
	if err := os.Remove(ch.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !ch.Existed {
		return nil
	}
	if ch.Symlink != "" {
		return os.Symlink(ch.Symlink, ch.Path)
	}
	return writeFileAtomic(ch.Path, ch.Content, restoredMode(ch.Mode))
}
