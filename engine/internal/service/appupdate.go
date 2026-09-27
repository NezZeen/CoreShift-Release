package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"coreshift/engine/internal/selfupdate"
	"coreshift/engine/internal/store"
)

// Self-update: the installed service looks for a new release once a day,
// downloads and verifies its installer and, with automatic updates on,
// runs it while the VPN is off. The installer replaces the service, which
// on its next start finishes the job: it starts the app again for the users
// who had it open and reconnects if the update interrupted a connection.
//
// On Android (Config.InstallUpdate) the download is offered to the user:
// the system's installer asks before it replaces the app, and the VPN stays
// on until it does.

const (
	appUpdateFirstCheck = 2 * time.Minute // the network may not be up at boot
	appUpdateEvery      = 24 * time.Hour
	appUpdateTick       = time.Minute
	appUpdateTimeout    = 15 * time.Minute // the whole download
)

// App update states.
const (
	UpdateOff         = "off"         // not an installed release build
	UpdateIdle        = "idle"        // up to date, or not checked yet
	UpdateChecking    = "checking"    //
	UpdateDownloading = "downloading" //
	UpdateReady       = "ready"       // downloaded and verified
	UpdateInstalling  = "installing"  // the installer is starting
	UpdateError       = "error"       //
)

// AppUpdate is the self-update state, for the UI.
type AppUpdate struct {
	State string `json:"state"`
	// Reason says why updates are off.
	Reason string `json:"reason,omitempty"`
	// Version and Build are the latest release found, when newer.
	Version   string    `json:"version,omitempty"`
	Build     int       `json:"build,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Published time.Time `json:"published,omitzero"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	Error     string    `json:"error,omitempty"`
	// Waiting: ready, and automatic installation waits for the VPN to be
	// turned off.
	Waiting bool `json:"waiting,omitempty"`
}

type appUpdater struct {
	mu        sync.Mutex
	state     AppUpdate
	rel       selfupdate.Release
	installer string // the verified download
	failed    string // releaseKey of an update that did not install
	checkNow  chan struct{}
}

// pendingUpdate is written before the installer runs, for the service it
// installs.
type pendingUpdate struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Label     string    `json:"label"`
	Reconnect bool      `json:"reconnect"`
	Sessions  []uint32  `json:"sessions,omitempty"`
	Started   time.Time `json:"started"`
}

func releaseKey(version string, build int) string { return fmt.Sprintf("%s+%d", version, build) }

func (s *Service) updatesDir() string { return filepath.Join(s.cfg.DataDir, "updates") }

// AppUpdateState returns the self-update state.
func (s *Service) AppUpdateState() AppUpdate {
	s.upd.mu.Lock()
	defer s.upd.mu.Unlock()
	st := s.upd.state
	if st.State == UpdateReady && !s.userInstalls() {
		st.Waiting = s.appUpdateSettings().Auto && s.connected() && s.upd.failed != releaseKey(st.Version, st.Build)
	}
	return st
}

// userInstalls reports that the system's installer asks the user.
func (s *Service) userInstalls() bool { return s.cfg.InstallUpdate != nil }

func (s *Service) setAppUpdate(f func(*AppUpdate)) {
	s.upd.mu.Lock()
	f(&s.upd.state)
	st := s.upd.state
	s.upd.mu.Unlock()
	s.hub.publish(Event{Kind: "app-update", Reason: st.State, Line: st.Version, Error: st.Error})
}

func (s *Service) appUpdateSettings() store.AppUpdate {
	if s.cfg.Store == nil {
		return store.Defaults().AppUpdate
	}
	return s.cfg.Store.Settings().AppUpdate
}

// connected reports whether the VPN is on or coming up.
func (s *Service) connected() bool {
	st := s.Status().State
	return st == Connected || st == Connecting || st == Disconnecting
}

// CheckAppUpdate asks the updater to check now. It answers at once; the
// result arrives as app-update events.
func (s *Service) CheckAppUpdate() error {
	if !s.cfg.SelfUpdate {
		return errors.New("self-update is off: " + s.AppUpdateState().Reason)
	}
	select {
	case s.upd.checkNow <- struct{}{}:
	default: // one is queued already
	}
	return nil
}

// RunAppUpdates checks for new releases and installs them until ctx ends.
func (s *Service) RunAppUpdates(ctx context.Context) {
	if !s.cfg.SelfUpdate {
		return
	}
	s.loadFailedUpdate()
	s.finishAppUpdate()
	next := time.Now().Add(s.cfg.updateFirstCheck)
	first := time.NewTimer(s.cfg.updateFirstCheck) // sooner than the next tick
	defer first.Stop()
	tick := time.NewTicker(s.cfg.updateTick)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		case <-tick.C:
		case <-s.upd.checkNow:
			next = time.Now()
		}
		if !time.Now().Before(next) {
			s.checkAppUpdate(ctx)
			next = time.Now().Add(appUpdateEvery)
		}
		s.maybeInstallAppUpdate()
	}
}

func (s *Service) checkAppUpdate(ctx context.Context) {
	source := s.appUpdateSettings().Source
	if source == "" {
		source = selfupdate.DefaultSource
	}
	src, err := selfupdate.ParseSource(source)
	if err != nil {
		s.setAppUpdate(func(u *AppUpdate) { u.State, u.Error = UpdateError, err.Error() })
		return
	}
	s.setAppUpdate(func(u *AppUpdate) { u.State, u.Error = UpdateChecking, "" })
	var rel selfupdate.Release
	err = s.viaProxyOrDirect(ctx, time.Minute, func(c *http.Client) error {
		var err error
		rel, err = s.cfg.checkRelease(ctx, c, src)
		return err
	})
	now := time.Now()
	if err != nil {
		s.setAppUpdate(func(u *AppUpdate) { u.State, u.Error, u.CheckedAt = UpdateError, err.Error(), now })
		return
	}
	if !rel.Newer(Version, BuildNumber()) {
		os.RemoveAll(filepath.Join(s.updatesDir(), "installers"))
		s.setAppUpdate(func(u *AppUpdate) {
			*u = AppUpdate{State: UpdateIdle, CheckedAt: now}
		})
		return
	}
	s.setAppUpdate(func(u *AppUpdate) {
		*u = AppUpdate{State: UpdateDownloading, Version: rel.Version, Build: rel.Build, Notes: rel.Notes,
			Published: rel.Published, CheckedAt: now}
	})
	dir := filepath.Join(s.updatesDir(), "installers")
	if err := os.MkdirAll(dir, 0o700); err == nil {
		// The installer runs as SYSTEM: nobody else may swap it.
		err = restrictDir(s.updatesDir())
	}
	var path string
	if err == nil {
		err = s.viaProxyOrDirect(ctx, appUpdateTimeout, func(c *http.Client) error {
			var err error
			path, err = s.cfg.downloadRelease(ctx, c, rel, dir)
			return err
		})
	}
	if err != nil {
		s.setAppUpdate(func(u *AppUpdate) { u.State, u.Error = UpdateError, err.Error() })
		return
	}
	s.upd.mu.Lock()
	s.upd.rel, s.upd.installer = rel, path
	s.upd.mu.Unlock()
	s.setAppUpdate(func(u *AppUpdate) { u.State = UpdateReady })
}

// maybeInstallAppUpdate installs a ready update when automatic updates are
// on and the VPN is off. An update that failed to install once is left to
// the user's button.
func (s *Service) maybeInstallAppUpdate() {
	s.upd.mu.Lock()
	ready := s.upd.state.State == UpdateReady && s.upd.failed != releaseKey(s.upd.state.Version, s.upd.state.Build)
	s.upd.mu.Unlock()
	if ready && s.appUpdateSettings().Auto && !s.connected() && !s.userInstalls() {
		s.installAppUpdate(false)
	}
}

// InstallAppUpdate installs the downloaded update now, on the user's
// request: a connection is closed for it and made again afterwards.
func (s *Service) InstallAppUpdate() error {
	if st := s.AppUpdateState(); st.State != UpdateReady {
		return errors.New("no update is ready to install")
	}
	return s.installAppUpdate(s.connected())
}

func (s *Service) installAppUpdate(reconnect bool) error {
	s.upd.mu.Lock()
	rel, path := s.upd.rel, s.upd.installer
	s.upd.mu.Unlock()
	fail := func(err error) error {
		s.setAppUpdate(func(u *AppUpdate) { u.State, u.Error = UpdateError, err.Error() })
		return err
	}
	// Checked again right before it runs as SYSTEM.
	if sum, err := selfupdate.FileSHA256(path); err != nil || sum != rel.SHA256 {
		return fail(errors.New("the downloaded update changed or is gone; it will be downloaded again"))
	}
	s.setAppUpdate(func(u *AppUpdate) { u.State, u.Error = UpdateInstalling, "" })
	p := pendingUpdate{
		From: releaseKey(Version, BuildNumber()), To: releaseKey(rel.Version, rel.Build), Label: rel.Label(),
		Reconnect: reconnect, Sessions: s.cfg.appSessions(), Started: time.Now(),
	}
	b, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(s.updatesDir(), "pending.json"), b, 0o600); err != nil {
		return fail(err)
	}
	if s.userInstalls() {
		// The user may still say no: the VPN stays until the update
		// replaces the app, and the update stays on offer.
		if err := s.cfg.launchInstaller(path, ""); err != nil {
			os.Remove(filepath.Join(s.updatesDir(), "pending.json"))
			return fail(fmt.Errorf("start the installer: %w", err))
		}
		s.setAppUpdate(func(u *AppUpdate) { u.State = UpdateReady })
		return nil
	}
	// Disconnecting first restores DNS at once; the installer stops the
	// service anyway.
	s.Disconnect()
	if err := s.cfg.launchInstaller(path, filepath.Join(s.updatesDir(), "install.log")); err != nil {
		os.Remove(filepath.Join(s.updatesDir(), "pending.json"))
		if reconnect {
			go s.ConnectSelected(context.Background())
		}
		return fail(fmt.Errorf("start the installer: %w", err))
	}
	return nil
}

// finishAppUpdate runs at start: after an update it brings back what the
// update interrupted, or reports that it did not install.
func (s *Service) finishAppUpdate() {
	path := filepath.Join(s.updatesDir(), "pending.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	os.Remove(path)
	var p pendingUpdate
	if json.Unmarshal(b, &p) != nil {
		return
	}
	if p.To != releaseKey(Version, BuildNumber()) && s.userInstalls() {
		return // declined: the next check offers it again
	}
	if p.To != releaseKey(Version, BuildNumber()) {
		s.upd.mu.Lock()
		s.upd.failed = p.To
		s.upd.mu.Unlock()
		os.WriteFile(filepath.Join(s.updatesDir(), "failed"), []byte(p.To), 0o600)
		s.setAppUpdate(func(u *AppUpdate) {
			u.State = UpdateError
			u.Error = fmt.Sprintf("update to %s did not install; see %s", p.Label, filepath.Join(s.updatesDir(), "install.log"))
		})
	} else {
		os.RemoveAll(filepath.Join(s.updatesDir(), "installers"))
		s.hub.publish(Event{Kind: "app-update", Reason: "installed", Line: p.Label})
	}
	if len(p.Sessions) > 0 {
		if err := s.cfg.startApp(p.Sessions); err != nil {
			s.hub.publish(Event{Kind: "app-update", Error: "start the app after the update: " + err.Error()})
		}
	}
	if p.Reconnect {
		go s.ConnectSelected(context.Background())
	}
}

func (s *Service) loadFailedUpdate() {
	if b, err := os.ReadFile(filepath.Join(s.updatesDir(), "failed")); err == nil {
		s.upd.mu.Lock()
		s.upd.failed = string(b)
		s.upd.mu.Unlock()
	}
}
