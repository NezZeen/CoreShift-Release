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
// The setups (linuxstack.go, detected on every Apply):
//   - systemd-resolved with /etc/resolv.conf pointing at its stub (Ubuntu,
//     Fedora, and NetworkManager on top of it): the TUN link gets our
//     servers and the "~." routing domain, so every query goes there.
//   - resolvconf / openresolv: a record of ours, exclusive where openresolv
//     allows it.
//   - netconfig (openSUSE): our servers handed to it as a service.
//   - NetworkManager writing /etc/resolv.conf itself (Debian without
//     resolved, Arch), or a plain file, symlink or not: we write the file,
//     and Keep writes it again whenever someone replaces it, say after a
//     DHCP renewal.
//
// With firewalld running, the TUN interface also goes into its trusted
// zone while the tunnel is up (runtime only): the TUN layer answers
// connections from the system through it, which the default zones refuse.
//
// Whatever the setup, the TUN layer also hijacks every port-53 packet
// routed into the tunnel, so a resolver the guard missed still cannot leak:
// the guard keeps lookups fast and tidy rather than being the only wall.

const (
	kindResolvedLink = "linux.resolved"
	kindResolvConf   = "linux.resolvconf"
	kindResolvconf   = "linux.resolvconf-record"
	kindNetconfig    = "linux.netconfig"
	kindFirewalld    = "linux.firewalld"
)

// maxResolvConf bounds a resolv.conf the journal may restore.
const maxResolvConf = 64 << 10

// trustedLinkDirs are where a symlinked /etc/resolv.conf may point: the
// files systemd-resolved, NetworkManager and resolvconf manage. A link
// anywhere else is neither replaced nor restored. /mnt/wsl holds the
// resolv.conf WSL generates for its distributions.
var trustedLinkDirs = []string{"/run/", "/var/run/", "/etc/resolvconf/", "/usr/lib/systemd/", "/lib/systemd/", "/mnt/wsl/"}

type commandRunner func(ctx context.Context, name string, args ...string) error

// inputRunner runs a command with input on its stdin.
type inputRunner func(ctx context.Context, input []byte, name string, args ...string) error

type resolvedLinkChange struct {
	Interface string `json:"interface"`
}

// resolvconfChange is our resolvconf record; netconfigChange our netconfig
// service; firewalldChange the TUN interface in a firewalld zone.
type resolvconfChange struct {
	Record string `json:"record"`
}

type netconfigChange struct {
	Interface string `json:"interface"`
}

type firewalldChange struct {
	Interface string `json:"interface"`
	Zone      string `json:"zone"`
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
	runInput       inputRunner
	resolvConfPath string
	// detect says what manages the system's DNS now.
	detect func() dnsStack
	// firewalld reports whether firewalld runs.
	firewalld func() bool
	// networkManager reports whether NetworkManager runs: it is told to
	// leave the TUN link alone.
	networkManager func() bool
	linkExists     func(name string) bool
	// selinux reports whether SELinux labels files here (Fedora, RHEL):
	// a resolv.conf the guard writes or restores is relabelled, as the
	// atomic write leaves it etc_t rather than net_conf_t, which
	// NetworkManager and the DHCP clients may not replace in enforcing mode.
	selinux func() bool
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
	if g.firewalld != nil && g.firewalld() {
		if err := g.journal.Record(kindFirewalld, firewalldChange{Interface: cfg.Interface, Zone: firewalldZone}); err != nil {
			return err
		}
		// Best effort: without it TCP through the TUN may stall, which the
		// health checks report, but DNS is still redirected.
		_ = g.run(ctx, "firewall-cmd", "--zone="+firewalldZone, "--change-interface="+cfg.Interface)
	}
	var err error
	switch g.detect() {
	case stackResolved:
		err = g.applyResolved(ctx, cfg)
	case stackResolvconf:
		err = g.applyResolvconf(ctx, cfg)
	case stackNetconfig:
		if err = g.applyNetconfig(ctx, cfg); err == nil && !g.resolvConfLeadsWith(cfg) {
			// netconfig took our service but did not put it first. With
			// NetworkManager its "auto" policy is "STATIC_FALLBACK
			// NetworkManager" and ignores every other service (openSUSE's
			// default desktop); with wicked it may rank ours after the
			// LAN's resolver, which the TUN routes leave outside the tunnel.
			// Write the file then, as where NetworkManager writes it.
			if err = g.applyResolvConf(ctx, cfg); err == nil {
				c := cfg
				g.written = &c
			}
		}
	default:
		if err = g.applyResolvConf(ctx, cfg); err == nil {
			c := cfg
			g.written = &c
		}
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

func (*linuxGuard) LinkBound() {}

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
	if err := g.applyResolvConf(ctx, cfg); err != nil {
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

// applyResolvconf adds a record of ours to resolvconf: exclusive (-x) and
// first (-m 0) with openresolv; Debian's resolvconf knows neither, and
// orders it by name (resolvconfRecord).
func (g *linuxGuard) applyResolvconf(ctx context.Context, cfg Config) error {
	rec := resolvconfRecord(cfg.Interface)
	if err := g.journal.Record(kindResolvconf, resolvconfChange{Record: rec}); err != nil {
		return err
	}
	in := renderResolvConf(cfg)
	if err := g.runInput(ctx, in, "resolvconf", "-a", rec, "-m", "0", "-x"); err != nil {
		if err2 := g.runInput(ctx, in, "resolvconf", "-a", rec); err2 != nil {
			return fmt.Errorf("dnsguard: resolvconf -a %s: %w", rec, errors.Join(err, err2))
		}
	}
	return nil
}

// applyNetconfig hands our servers to netconfig as a service of our own.
func (g *linuxGuard) applyNetconfig(ctx context.Context, cfg Config) error {
	if err := g.journal.Record(kindNetconfig, netconfigChange{Interface: cfg.Interface}); err != nil {
		return err
	}
	if err := g.runInput(ctx, renderNetconfig(cfg), "netconfig", "modify", "-s", "coreshift", "-i", cfg.Interface); err != nil {
		return fmt.Errorf("dnsguard: netconfig modify: %w", err)
	}
	return nil
}

// resolvConfLeadsWith reports whether resolv.conf's first nameserver is the
// tunnel's: glibc asks the first one first.
func (g *linuxGuard) resolvConfLeadsWith(cfg Config) bool {
	b, err := os.ReadFile(g.resolvConfPath)
	if err != nil || len(cfg.Servers) == 0 {
		return false
	}
	ns := resolvConfNameservers(b)
	return len(ns) > 0 && ns[0] == cfg.Servers[0]
}

func (g *linuxGuard) applyResolvConf(ctx context.Context, cfg Config) error {
	ch, err := snapshotFile(g.resolvConfPath)
	if err != nil {
		return fmt.Errorf("dnsguard: read %s: %w", g.resolvConfPath, err)
	}
	if ch.Symlink != "" && !g.trustedLink(ch.Path, ch.Symlink) {
		// Restoring an unusual link would have to be trusted later; better
		// to touch nothing. Every query still reaches the tunnel's DNS:
		// the TUN layer hijacks port 53 whatever the resolver.
		return nil
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
	g.relabel(ctx, ch.Path)
	return nil
}

// relabel gives path the SELinux label the policy wants there, where
// SELinux runs. Best effort: a wrong label still reads, it only stops
// confined programs from replacing the file later.
func (g *linuxGuard) relabel(ctx context.Context, path string) {
	if g.selinux != nil && g.selinux() && filepath.IsAbs(path) {
		_ = g.run(ctx, "restorecon", path)
	}
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
	case kindResolvconf:
		var ch resolvconfChange
		if err := json.Unmarshal(c.Data, &ch); err != nil {
			return fmt.Errorf("%w: %v", ErrRejected, err)
		}
		iface, ok := strings.CutPrefix(ch.Record, "tun.")
		if !ok || !validInterfaceName(iface) {
			return fmt.Errorf("%w: resolvconf record %q", ErrRejected, ch.Record)
		}
		// Best effort, and never left to retry: the records live in /run,
		// which a reboot empties, and a record already gone fails the call.
		_ = g.run(ctx, "resolvconf", "-d", ch.Record)
		return nil
	case kindNetconfig:
		var ch netconfigChange
		if err := json.Unmarshal(c.Data, &ch); err != nil {
			return fmt.Errorf("%w: %v", ErrRejected, err)
		}
		if !validInterfaceName(ch.Interface) {
			return fmt.Errorf("%w: interface name %q", ErrRejected, ch.Interface)
		}
		// As with resolvconf: netconfig keeps services in /run.
		_ = g.run(ctx, "netconfig", "remove", "-s", "coreshift", "-i", ch.Interface)
		return nil
	case kindFirewalld:
		var ch firewalldChange
		if err := json.Unmarshal(c.Data, &ch); err != nil {
			return fmt.Errorf("%w: %v", ErrRejected, err)
		}
		if !validInterfaceName(ch.Interface) || ch.Zone != firewalldZone {
			return fmt.Errorf("%w: firewalld %q in zone %q", ErrRejected, ch.Interface, ch.Zone)
		}
		// Runtime only: a reload or reboot drops it anyway.
		_ = g.run(ctx, "firewall-cmd", "--zone="+ch.Zone, "--remove-interface="+ch.Interface)
		return nil
	case kindResolvConf:
		var ch resolvConfChange
		if err := json.Unmarshal(c.Data, &ch); err != nil {
			return fmt.Errorf("%w: %v", ErrRejected, err)
		}
		if err := g.checkResolvConfChange(ch); err != nil {
			return err
		}
		if err := restoreResolvConf(ch); err != nil {
			return err
		}
		if _, err := os.Lstat(ch.Path); err == nil {
			g.relabel(ctx, ch.Path)
		}
		return nil
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
