//go:build android

// Package mobile runs the CoreShift engine inside the Android app. It is
// bound with gomobile (packaging/android/build.ps1); the app implements
// Platform in Kotlin.
//
// The engine is the desktop service, unchanged: the same store, supervisor
// and auto-swap, and the same HTTP API on 127.0.0.1, which the Flutter UI
// reads through dataDir/api.json just as on the desktop. What differs is
// the TUN + DNS layer: sing-box runs inside the app on the TUN of Android's
// VpnService, and the VPN leaves the app itself out, so the cores, which
// the app starts as programs, reach their servers directly.
package mobile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/service"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/subscription"

	// Needed by the code gomobile generates.
	_ "golang.org/x/mobile/bind"
)

// Platform is implemented by the app.
type Platform interface {
	// OpenTun starts the VpnService with cfg and returns its TUN's file
	// descriptor, which the VpnService keeps owning.
	OpenTun(cfg *TunConfig) (int32, error)
	// CloseTun stops the VpnService.
	CloseTun()
	// InstallUpdate hands the verified APK at path to Android's installer,
	// which asks the user to update the app.
	InstallUpdate(path string) error
	// StateChanged reports the connection for the notification and the
	// quick settings tile: the state ("idle", "connecting", "connected",
	// "disconnecting", "failed"), the server and when it connected (Unix
	// milliseconds, 0 when not connected).
	StateChanged(state, node string, sinceMillis int64)
	// Traffic reports the speed every second while connected, in bytes
	// per second.
	Traffic(downRate, upRate int64)
}

// Status is the connection as the notification and the tile show it.
type Status struct {
	State       string
	Node        string
	SinceMillis int64
}

func statusOf(svc *service.Service) *Status {
	st := svc.Status()
	var since int64
	if !st.Since.IsZero() {
		since = st.Since.UnixMilli()
	}
	return &Status{State: string(st.State), Node: st.Node, SinceMillis: since}
}

// CurrentStatus returns the connection now; idle before the engine runs.
func CurrentStatus() *Status {
	mu.Lock()
	e := running
	mu.Unlock()
	if e == nil {
		return &Status{State: string(service.Idle)}
	}
	return statusOf(e.svc)
}

// Connect connects the selected server, from the quick settings tile. It
// blocks until connected or failed.
func Connect() error {
	mu.Lock()
	e := running
	mu.Unlock()
	if e == nil {
		return errors.New("the engine is not running")
	}
	return e.svc.ConnectSelected(e.ctx)
}

// report passes state changes and the speed to the app until ctx ends.
func report(ctx context.Context, svc *service.Service, p Platform) {
	events, unsubscribe := svc.Subscribe(false)
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-events:
			switch e.Kind {
			case "state":
				st := statusOf(svc)
				p.StateChanged(st.State, st.Node, st.SinceMillis)
			case "traffic":
				p.Traffic(e.DownRate, e.UpRate)
			}
		}
	}
}

// TunConfig is what the VpnService is built with.
type TunConfig struct {
	Address4 string // "172.19.0.1/30"
	Address6 string // empty when IPv6 is off
	DNS      string // the tunnel's resolver, "172.19.0.2"
	MTU      int32
}

// coreFiles are the cores as the APK ships them: Android lets apps run
// programs only from their native library directory, as lib*.so files.
var coreFiles = map[core.Kind]string{
	core.Xray:    "libxray.so",
	core.SingBox: "libsingbox.so",
	core.Mihomo:  "libmihomo.so",
}

type engine struct {
	ctx     context.Context
	cancel  context.CancelFunc
	svc     *service.Service
	srv     *http.Server
	apiFile string
}

var (
	mu      sync.Mutex
	running *engine
)

// Start runs the engine: settings and subscriptions live in dataDir, the
// cores come from libDir (the app's native library directory). It writes
// dataDir/api.json, the address and token of the API. deviceID (the
// ANDROID_ID), osVersion and model identify the phone to subscription
// panels that limit devices. Starting a running engine does nothing.
func Start(dataDir, libDir, deviceID, osVersion, model string, p Platform) error {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		return nil
	}
	subscription.SetDeviceInfo(deviceID, osVersion, model)

	bins := map[core.Kind]string{}
	for k, name := range coreFiles {
		path := filepath.Join(libDir, name)
		if _, err := os.Stat(path); err == nil {
			bins[k] = path
		}
	}
	st, err := service.OpenStore(dataDir, store.Options{})
	if err != nil && !errors.Is(err, store.ErrReset) {
		return err
	}
	tun := &vpnTUN{platform: p, dir: filepath.Join(dataDir, "tun")}
	svc, err := service.New(service.Config{
		DataDir:         dataDir,
		Binaries:        bins,
		Store:           st,
		TUNLayer:        tun,
		SystemResolvers: currentNetwork.resolvers,
		// The app is outside the VPN: pings and lookups of servers leave
		// by the default network.
		PhysicalBind:  func() (ping.Bind, error) { return ping.Bind{}, nil },
		AppOutsideVPN: true,
		HostIPv6:      currentNetwork.hasIPv6,
		// Release builds look for new APKs; the user installs them.
		SelfUpdate:    service.Version != "dev",
		InstallUpdate: p.InstallUpdate,
		// The app is often open for a moment only.
		FirstUpdateCheck: 20 * time.Second,
	})
	if err != nil {
		return err
	}
	tun.log = svc.Log

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := netip.MustParseAddrPort(ln.Addr().String())
	var raw [32]byte
	rand.Read(raw[:])
	token := hex.EncodeToString(raw[:])
	info, _ := json.Marshal(map[string]string{"address": "http://" + addr.String(), "token": token})
	apiFile := filepath.Join(dataDir, "api.json")
	if err := os.WriteFile(apiFile, info, 0o600); err != nil {
		ln.Close()
		return fmt.Errorf("write %s: %w", apiFile, err)
	}
	srv := &http.Server{Handler: service.NewAPI(svc, token, addr), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)

	ctx, cancel := context.WithCancel(context.Background())
	go st.RunUpdater(ctx, time.Minute)
	go svc.RunAppUpdates(ctx)
	go report(ctx, svc, p)
	running = &engine{ctx: ctx, cancel: cancel, svc: svc, srv: srv, apiFile: apiFile}
	return nil
}

// Stop disconnects and stops the engine.
func Stop() {
	mu.Lock()
	e := running
	running = nil
	mu.Unlock()
	if e == nil {
		return
	}
	e.svc.Disconnect()
	e.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if e.srv.Shutdown(ctx) != nil {
		e.srv.Close() // event streams never go idle
	}
	os.Remove(e.apiFile)
}

// AutoConnectEnabled reports whether the settings ask to connect on start
// ("Автозапуск"): when CoreShift opens and when the phone starts.
func AutoConnectEnabled() bool {
	mu.Lock()
	e := running
	mu.Unlock()
	return e != nil && e.svc.AutoConnectEnabled()
}

// AutoConnect connects the selected node when the settings ask for it and
// nothing is connected yet, retrying while the network comes up. It blocks
// and reports whether the VPN is up at the end.
func AutoConnect() bool {
	mu.Lock()
	e := running
	mu.Unlock()
	if e == nil {
		return false
	}
	e.svc.AutoConnect(e.ctx)
	return e.svc.Status().State == service.Connected
}

// Disconnect turns the VPN off, from the notification or the quick
// settings tile, without the UI.
func Disconnect() {
	mu.Lock()
	e := running
	mu.Unlock()
	if e != nil {
		e.svc.Disconnect()
	}
}

// Revoked tells the engine that Android took the VPN away: the user
// started another VPN app or turned CoreShift off in the system settings.
func Revoked() {
	currentTUN.revoke()
}
