package supervisor

import (
	"coreshift/engine/internal/core"
	"coreshift/engine/internal/socksgate"
)

// Inbound is who besides the TUN layer may use Listen while there is no
// TUN layer: the user's programs, through the proxy of the system or set
// up by hand.
type Inbound struct {
	// Guest are credentials Listen takes besides Auth: those shown to the
	// user, for an inbound every app on the device reaches (Android).
	Guest core.SOCKSAuth
	// HTTP serves HTTP proxy requests on Listen too, with Auth or Guest
	// as their credentials unless OpenInbound or OpenHTTP.
	HTTP bool
	// OpenHTTP lets HTTP in without credentials while SOCKS needs them.
	OpenHTTP bool
}

// gateConfig is what Connect opens Listen with.
func (c Config) gateConfig() socksgate.Config {
	return socksgate.Config{
		Listen: c.Listen, Auth: c.Auth, Open: c.OpenInbound,
		Guest: c.Inbound.Guest, HTTP: c.Inbound.HTTP, OpenHTTP: c.Inbound.OpenHTTP,
	}
}
