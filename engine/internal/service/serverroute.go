package service

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"

	"coreshift/engine/internal/serverroute"
)

// The connected server's own address is routed around the tunnel by the
// engine itself (internal/serverroute) rather than by the TUN layer's
// configuration, so the layer's config does not depend on the server and a
// switch of servers keeps the layer — its interface, the DNS redirect and
// the fake-IP records applications hold — running (connectLocked). Where a
// route cannot be managed (an IPv6 server, no rights), the TUN layer
// excludes the server as before (Options.BypassAddresses), at the price of
// a restart on every switch.
//
// The route is written to disk while held, so Recover can remove what a
// crashed run left: a stale route would send the old server's address past
// a later tunnel.

// serverRoutePath is where the held route is kept for Recover; "" without a
// data directory (tests without one).
func (s *Service) serverRoutePath() string {
	if s.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.DataDir, "serverroute.json")
}

// addServerRoute routes the server's ip around the tunnel, replacing the
// route of the server connected before. A no-op where the platform keeps
// the app, and so the cores, out of the VPN (Android), or where routes are
// not managed (no routeAdd). Called with s.op held.
func (s *Service) addServerRoute(ip netip.Addr) error {
	if s.cfg.AppOutsideVPN || !ip.IsValid() {
		return nil
	}
	add, del := s.cfg.routeAdd, s.cfg.routeDel
	if add == nil {
		return nil // tests without a route manager
	}
	if s.hasServerRoute {
		if err := del(s.serverRoute); err != nil {
			s.Log("tun", "removing the old server's route: "+err.Error())
		}
		s.hasServerRoute = false
		s.removeServerRouteFile()
	}
	r, err := add(ip)
	if err != nil {
		return err
	}
	if path := s.serverRoutePath(); path != "" {
		if b, err := json.Marshal(r); err == nil {
			// Best effort: the route exists without the note, and a crash
			// leaves it for as long as the system keeps it (a reboot clears).
			_ = os.WriteFile(path, b, 0o600)
		}
	}
	s.serverRoute, s.hasServerRoute = r, true
	return nil
}

// dropServerRoute removes the route addServerRoute made, if any. Called
// with s.op held.
func (s *Service) dropServerRoute() {
	if !s.hasServerRoute {
		return
	}
	s.hasServerRoute = false
	if s.cfg.routeDel != nil {
		if err := s.cfg.routeDel(s.serverRoute); err != nil {
			s.Log("tun", "removing the server's route: "+err.Error())
		}
	}
	s.removeServerRouteFile()
}

func (s *Service) removeServerRouteFile() {
	if path := s.serverRoutePath(); path != "" {
		os.Remove(path)
	}
}

// recoverServerRoute removes a route a crashed run left behind.
func (s *Service) recoverServerRoute() {
	path := s.serverRoutePath()
	if path == "" || s.cfg.routeDel == nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return // nothing was left
	}
	var r serverroute.Route
	if json.Unmarshal(b, &r) == nil && r.IP.IsValid() {
		// Best effort: a route the system no longer has is no error (Del).
		_ = s.cfg.routeDel(r)
	}
	os.Remove(path)
}
