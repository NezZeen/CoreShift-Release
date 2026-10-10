// Package sysproxy sets the proxy of the user's system to CoreShift's
// local port and puts back what was there before: «Системный прокси», for
// computers where the TUN does not come up (an antivirus, a corporate PC,
// Wintun blocked). Browsers and most programs then go through the VPN
// without being set up one by one.
//
// It runs as the user, in the user's session, never in the daemon: the
// proxy is the user's own setting. On Windows it is the per-user WinINet
// setting (HKCU), which a service running as LocalSystem does not have;
// on Linux it is GNOME's dconf or KDE's kioslaverc, reached through the
// session's D-Bus, which the hardened root daemon (no CAP_SETUID,
// ProtectHome=read-only) cannot reach either. The app, running as the user,
// calls `coreshiftd sysproxy`, which is this package.
//
// Nothing may leave the computer with a proxy pointing at a port where
// nothing listens: every site would stop opening. So:
//
//   - The settings found are written to a journal (Journal, in the user's
//     own folder) before anything changes, and the journal is what
//     Restore puts back. A journal left by a run that lost its own is
//     never taken for the user's: settings that are already CoreShift's
//     count as "no proxy" (Backend.Direct).
//   - Restore puts back only what is still CoreShift's: a proxy the user
//     changed meanwhile is left as it is.
//   - The app restores on disconnect and when it starts; a guard process
//     (Guard), started with the proxy, restores when the port has been
//     dead for a while: the daemon or the app crashed, or the VPN went
//     down without the app.
//   - A sign-in entry (Autostart: RunOnce on Windows, an XDG autostart
//     file on Linux) restores at the next sign-in after a crash, a power
//     cut or a reboot, unless the port is alive by then (Logon).
package sysproxy

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"coreshift/engine/internal/socksgate"
)

// Settings are one backend's proxy settings, key → value in the backend's
// own notation, as Read returns them and Write takes them.
type Settings map[string]string

// Backend is one place where the system keeps its proxy: Windows' WinINet,
// GNOME, KDE.
type Backend interface {
	// Name is how the journal and the report call it: "windows", "gnome",
	// "kde".
	Name() string
	// Read returns the settings now.
	Read(ctx context.Context) (Settings, error)
	// Write makes s the settings, and tells the system's programs.
	Write(ctx context.Context, s Settings) error
	// Proxy is the settings that send traffic to addr, the local network
	// and loopback excepted.
	Proxy(addr netip.AddrPort) Settings
	// Direct is the settings without a proxy, for when the previous ones
	// are not known.
	Direct() Settings
	// Owns reports whether cur are still ours, as Proxy made them.
	Owns(cur, ours Settings) bool
}

// Autostart is the entry that runs Logon at the user's next sign-in.
type Autostart interface {
	Set() error
	Clear() error
}

// Report says what an action did, for the app's journal: it is printed by
// `coreshiftd sysproxy` as JSON.
type Report struct {
	Action string `json:"action"`
	// Backends are the places found to keep a proxy; none means the
	// desktop is not supported (Linux without GNOME or KDE).
	Backends []string `json:"backends"`
	// Applied and Restored are the backends set and put back.
	Applied  []string `json:"applied,omitempty"`
	Restored []string `json:"restored,omitempty"`
	// Changed are the backends whose proxy the user changed meanwhile:
	// left as they are.
	Changed []string `json:"changed,omitempty"`
	// Alive: at sign-in the port still answered, so the proxy was left.
	Alive bool `json:"alive,omitempty"`
	// Running: a guard already watches the proxy.
	Running bool     `json:"running,omitempty"`
	Errors  []string `json:"errors,omitempty"`
	// Pending: the journal still holds settings to put back.
	Pending bool `json:"pending"`
}

func (r *Report) fail(b string, err error) {
	if b != "" {
		r.Errors = append(r.Errors, b+": "+err.Error())
	} else {
		r.Errors = append(r.Errors, err.Error())
	}
}

// Manager sets and restores the proxy of the backends found.
type Manager struct {
	// Journal is the file of the settings to put back.
	Journal  string
	Backends []Backend
	// Autostart, if set, restores at the next sign-in.
	Autostart Autostart
	// Alive reports whether CoreShift's gate listens at addr; nil asks it
	// (socksgate.IsGate).
	Alive func(addr string) bool
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) alive(addr string) bool {
	if m.Alive != nil {
		return m.Alive(addr)
	}
	// Not merely a port that answers: another program may have taken it
	// while CoreShift was down.
	return socksgate.IsGate(addr, 2*time.Second)
}

func (m *Manager) backend(name string) Backend {
	for _, b := range m.Backends {
		if b.Name() == name {
			return b
		}
	}
	return nil
}

func (m *Manager) report(action string) Report {
	r := Report{Action: action, Backends: []string{}}
	for _, b := range m.Backends {
		r.Backends = append(r.Backends, b.Name())
	}
	return r
}

// ErrNoDesktop: no backend was found.
var ErrNoDesktop = errors.New("no supported desktop: neither GNOME nor KDE settings were found")

// Apply sets every backend's proxy to addr. The settings found are
// journaled first; those of a backend already in the journal stay as
// they were journaled, so applying twice never takes CoreShift's own
// proxy for the user's.
func (m *Manager) Apply(ctx context.Context, addr netip.AddrPort) Report {
	r := m.report("apply")
	if len(m.Backends) == 0 {
		r.fail("", ErrNoDesktop)
		return r
	}
	j, err := loadJournal(m.Journal)
	if err != nil {
		r.fail("", err)
	}
	j.Addr, j.At = addr.String(), m.now().UTC()
	var touched []string
	for _, b := range m.Backends {
		ours := b.Proxy(addr)
		if e := j.entry(b.Name()); e != nil {
			e.Ours, e.Restoring = ours, false
			touched = append(touched, b.Name())
			continue
		}
		cur, err := b.Read(ctx)
		if err != nil {
			r.fail(b.Name(), err)
			continue
		}
		prev := cur
		if b.Owns(cur, ours) {
			// Left by a run whose journal is gone: not the user's.
			prev = b.Direct()
		}
		j.Entries = append(j.Entries, Entry{Backend: b.Name(), Previous: prev, Ours: ours})
		touched = append(touched, b.Name())
	}
	if len(touched) == 0 {
		r.Pending = len(j.Entries) > 0
		return r
	}
	// Nothing changes before the journal says how to undo it.
	if err := saveJournal(m.Journal, j); err != nil {
		r.fail("", fmt.Errorf("journal: %w", err))
		return r
	}
	if m.Autostart != nil {
		if err := m.Autostart.Set(); err != nil {
			r.fail("", fmt.Errorf("sign-in entry: %w", err))
		}
	}
	for _, name := range touched {
		b, e := m.backend(name), j.entry(name)
		if err := b.Write(ctx, e.Ours); err != nil {
			r.fail(name, err)
			// Whatever part of it got written goes back.
			if rerr := b.Write(ctx, e.Previous); rerr == nil {
				j.drop(name)
			}
			continue
		}
		r.Applied = append(r.Applied, name)
	}
	m.finish(&j, &r)
	return r
}

// Restore puts back the journaled settings of every backend whose proxy
// is still CoreShift's; one the user changed meanwhile is left as it is.
// Settings that could not be put back stay in the journal for later.
func (m *Manager) Restore(ctx context.Context) Report {
	r := m.report("restore")
	j, err := loadJournal(m.Journal)
	if err != nil {
		r.fail("", err)
	}
	if len(j.Entries) == 0 {
		m.finish(&j, &r)
		return r
	}
	for _, e := range slices.Clone(j.Entries) {
		b := m.backend(e.Backend)
		if b == nil {
			// Another desktop than the one it was set in: kept for it.
			r.fail(e.Backend, errors.New("not available in this session"))
			continue
		}
		cur, err := b.Read(ctx)
		if err != nil {
			r.fail(e.Backend, err)
			continue
		}
		if !e.Restoring && !b.Owns(cur, e.Ours) {
			r.Changed = append(r.Changed, e.Backend)
			j.drop(e.Backend)
			continue
		}
		j.entry(e.Backend).Restoring = true
		if err := saveJournal(m.Journal, j); err != nil {
			r.fail(e.Backend, fmt.Errorf("journal: %w", err))
			continue
		}
		if err := b.Write(ctx, e.Previous); err != nil {
			r.fail(e.Backend, err)
			continue
		}
		r.Restored = append(r.Restored, e.Backend)
		j.drop(e.Backend)
	}
	m.finish(&j, &r)
	return r
}

// Logon runs at sign-in: it restores unless the port the proxy points at
// answers, in which case the VPN is already up again and the sign-in
// entry is set for the next time.
func (m *Manager) Logon(ctx context.Context) Report {
	j, err := loadJournal(m.Journal)
	if err == nil && len(j.Entries) > 0 && j.Addr != "" && m.alive(j.Addr) {
		r := m.report("logon")
		r.Alive, r.Pending = true, true
		if m.Autostart != nil {
			if err := m.Autostart.Set(); err != nil {
				r.fail("", fmt.Errorf("sign-in entry: %w", err))
			}
		}
		return r
	}
	r := m.Restore(ctx)
	r.Action = "logon"
	return r
}

// Status reports whether the journal holds settings to put back.
func (m *Manager) Status() Report {
	r := m.report("status")
	j, err := loadJournal(m.Journal)
	if err != nil {
		r.fail("", err)
	}
	r.Pending = len(j.Entries) > 0
	return r
}

// Guard waits while the proxy is set, checking every so often that its
// port answers, and restores once it has not for grace: the daemon or the
// app crashed, or the VPN went down while the app was closed. A reconnect
// closes the port for a moment, hence the grace. It returns when the
// journal is gone (the app restored), after restoring, or when ctx ends.
func (m *Manager) Guard(ctx context.Context, every, grace time.Duration) Report {
	t := time.NewTicker(every)
	defer t.Stop()
	var dead time.Time
	for {
		select {
		case <-ctx.Done():
			r := m.Status()
			r.Action = "guard"
			return r
		case <-t.C:
		}
		j, err := loadJournal(m.Journal)
		if err != nil || len(j.Entries) == 0 || j.Addr == "" {
			r := m.report("guard")
			r.Pending = len(j.Entries) > 0
			return r
		}
		if m.alive(j.Addr) {
			dead = time.Time{}
			continue
		}
		now := m.now()
		if dead.IsZero() {
			dead = now
			continue
		}
		if now.Sub(dead) >= grace {
			r := m.Restore(ctx)
			r.Action = "guard"
			return r
		}
	}
}

// GuardOnce is Guard, unless another guard watches already: then it
// says so and returns.
func (m *Manager) GuardOnce(ctx context.Context, every, grace time.Duration) Report {
	release, ok, err := lockFile(m.Journal + ".guard")
	if err != nil || !ok {
		r := m.Status()
		r.Action, r.Running = "guard", !ok && err == nil
		if err != nil {
			r.fail("", err)
		}
		return r
	}
	defer release()
	return m.Guard(ctx, every, grace)
}

// finish saves what is left of j, or removes the journal and the sign-in
// entry when nothing is.
func (m *Manager) finish(j *Journal, r *Report) {
	r.Pending = len(j.Entries) > 0
	if r.Pending {
		if err := saveJournal(m.Journal, *j); err != nil {
			r.fail("", fmt.Errorf("journal: %w", err))
		}
		return
	}
	if err := removeJournal(m.Journal); err != nil {
		r.fail("", fmt.Errorf("journal: %w", err))
	}
	if m.Autostart != nil {
		if err := m.Autostart.Clear(); err != nil {
			r.fail("", fmt.Errorf("sign-in entry: %w", err))
		}
	}
}

// sameKeys reports whether cur has every one of keys as ours has it.
func sameKeys(cur, ours Settings, keys ...string) bool {
	for _, k := range keys {
		if cur[k] != ours[k] {
			return false
		}
	}
	return true
}
