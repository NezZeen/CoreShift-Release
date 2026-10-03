package core

import (
	"crypto/rand"
	"encoding/hex"
	"net/netip"
	"net/url"
)

// SOCKSAuth is the username and password of the cores' SOCKS inbound.
//
// Without them any program on the device could use the inbound: on Android
// even an app the user keeps out of the VPN, which would both bypass the
// user's choice and see that a VPN runs. The service makes new ones each
// start and gives them to the TUN layer and its own HTTP clients only.
type SOCKSAuth struct {
	User, Pass string
}

// NewSOCKSAuth returns random credentials.
func NewSOCKSAuth() SOCKSAuth {
	return SOCKSAuth{User: randomHex(8), Pass: randomHex(16)}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Set reports whether there are credentials; without, the inbound is open.
func (a SOCKSAuth) Set() bool { return a.User != "" }

// ProxyURL is the inbound at ap as an HTTP client's proxy, credentials
// included. The URL holds the password: it must never be printed.
func (a SOCKSAuth) ProxyURL(ap netip.AddrPort) *url.URL {
	u := &url.URL{Scheme: "socks5", Host: ap.String()}
	if a.Set() {
		u.User = url.UserPassword(a.User, a.Pass)
	}
	return u
}

// String keeps the password out of logs that print the struct.
func (a SOCKSAuth) String() string {
	if !a.Set() {
		return "no SOCKS credentials"
	}
	return "SOCKS credentials (hidden)"
}

// GoString is String, for %#v.
func (a SOCKSAuth) GoString() string { return a.String() }
