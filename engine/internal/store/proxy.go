package store

import (
	"crypto/rand"
	"errors"
	"strings"
)

// ProxySettings are about the local proxy served without the TUN layer
// (TUN off): SOCKS5 and HTTP on one port of 127.0.0.1.
type ProxySettings struct {
	// System (Windows, Linux) has the app set the proxy of the system to
	// that port while connected, and put back what was there before: for
	// computers where the TUN does not come up. Off by default.
	System bool `json:"system"`
	// User and Pass (Android) are what apps must give the port: there any
	// app on the phone reaches 127.0.0.1. Made once, at random, by the
	// engine (WithAuth); the app shows them to be copied.
	User string `json:"user"`
	Pass string `json:"pass"`
	// OpenHTTP (Android) lets HTTP proxy requests in without credentials,
	// for the Wi-Fi proxy setting, which has no field for them: then any
	// app on the phone may use the proxy. Off by default.
	OpenHTTP bool `json:"open_http"`
}

// HasAuth reports whether the credentials are there.
func (p ProxySettings) HasAuth() bool { return p.User != "" && p.Pass != "" }

// WithAuth returns p with new random credentials.
func (p ProxySettings) WithAuth() ProxySettings {
	p.User = "cs" + randomText(6, lowerDigits)
	p.Pass = randomText(20, letterDigits)
	return p
}

const (
	lowerDigits  = "abcdefghijklmnopqrstuvwxyz0123456789"
	letterDigits = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
)

// randomText is n characters of alphabet, at random: letters and digits
// only, so the credentials go into a tg:// link and a form as they are.
func randomText(n int, alphabet string) string {
	out := make([]byte, 0, n)
	// Bytes past the last whole multiple of the alphabet are dropped, so
	// every character is as likely.
	limit := 256 - 256%len(alphabet)
	var buf [64]byte
	for len(out) < n {
		rand.Read(buf[:])
		for _, b := range buf {
			if int(b) < limit && len(out) < n {
				out = append(out, alphabet[int(b)%len(alphabet)])
			}
		}
	}
	return string(out)
}

// normalize checks the credentials: both or neither, at most 255 bytes
// (SOCKS5), no colon in the user (HTTP Basic) and nothing but printable
// characters.
func (p ProxySettings) normalize() (ProxySettings, error) {
	p.User, p.Pass = strings.TrimSpace(p.User), strings.TrimSpace(p.Pass)
	var errs []error
	if (p.User == "") != (p.Pass == "") {
		errs = append(errs, errors.New("proxy: user and pass go together"))
	}
	if len(p.User) > 255 || len(p.Pass) > 255 {
		errs = append(errs, errors.New("proxy: user and pass are at most 255 bytes"))
	}
	if strings.Contains(p.User, ":") {
		errs = append(errs, errors.New("proxy.user: no colon"))
	}
	if !printable(p.User) || !printable(p.Pass) {
		errs = append(errs, errors.New("proxy: user and pass must be printable characters"))
	}
	return p, errors.Join(errs...)
}

func printable(s string) bool {
	for _, r := range s {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return true
}
