package service

import (
	"fmt"

	"coreshift/engine/internal/store"
	"coreshift/engine/internal/supervisor"
)

// The proxy without the TUN layer ("Все приложения через VPN" off): the
// SOCKS port of the connection serves the user's programs, as SOCKS5 and
// as an HTTP proxy, on 127.0.0.1.
//
// On the desktop the port is open: programs are set to it by hand, or the
// app sets the proxy of the system to it (Options.SystemProxy; the app
// does it as the user, with `coreshiftd sysproxy`, see the sysproxy
// package). On Android ("Прокси без VPN") any app on the phone reaches
// 127.0.0.1, so the port takes only the credentials the app shows to the
// user (Options.ProxyAuth), besides those of the engine's own clients;
// HTTP without them only when the user allowed it (ProxyOpenHTTP), for
// the Wi-Fi proxy setting, which has no field for them.

// inbound is who may use the SOCKS port of a connection made with o.
func (s *Service) inbound(o Options) supervisor.Inbound {
	switch {
	case o.TUN:
		// Only the TUN layer, with the engine's credentials.
		return supervisor.Inbound{}
	case !s.cfg.AppOutsideVPN:
		return supervisor.Inbound{HTTP: true}
	}
	return supervisor.Inbound{Guest: o.ProxyAuth, HTTP: true, OpenHTTP: o.ProxyOpenHTTP}
}

// proxyUp tells the journal that a connection without the TUN layer
// serves the user's programs now, and how. Never the credentials:
// a journal is copied for support.
func (s *Service) proxyUp(o Options) {
	s.proxyOpen.Store(true)
	addr := s.cfg.Listen
	var line string
	switch {
	case !s.cfg.AppOutsideVPN && o.SystemProxy:
		line = fmt.Sprintf("прокси SOCKS5 и HTTP на %s открыт; системный прокси настроит приложение", addr)
	case !s.cfg.AppOutsideVPN:
		line = fmt.Sprintf("прокси SOCKS5 и HTTP на %s открыт для программ, настроенных на него", addr)
	case !o.ProxyAuth.Set():
		line = fmt.Sprintf("прокси без VPN на %s: нет логина и пароля, приложения не смогут им пользоваться", addr)
	case o.ProxyOpenHTTP:
		line = fmt.Sprintf("прокси без VPN на %s: SOCKS5 по логину и паролю, HTTP без пароля — им может пользоваться любое приложение на телефоне", addr)
	default:
		line = fmt.Sprintf("прокси без VPN на %s: SOCKS5 и HTTP по логину и паролю из настроек", addr)
	}
	s.hub.publish(Event{Kind: "proxy", Reason: "up", Line: line})
}

// proxyDown tells the journal that the port is closed, once per proxyUp.
func (s *Service) proxyDown() {
	if !s.proxyOpen.Swap(false) {
		return
	}
	line := "прокси закрыт"
	if s.cfg.AppOutsideVPN {
		line = "прокси без VPN выключен"
	}
	s.hub.publish(Event{Kind: "proxy", Reason: "down", Line: line})
}

// ensureProxyAuth gives the store the credentials of Android's proxy
// without the VPN, made once, at random, when it has none. A failure to
// save leaves the proxy without them: no app gets in, rather than one
// guessing them.
func ensureProxyAuth(st *store.Store, android bool) {
	set := st.Settings()
	if !android || set.Proxy.HasAuth() {
		return
	}
	set.Proxy = set.Proxy.WithAuth()
	_, _ = st.SetSettings(set)
}
