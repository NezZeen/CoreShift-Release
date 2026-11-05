//go:build !windows && !linux

package serverroute

import "errors"

func addRoute(Route) error {
	return errors.New("server routes are not supported on this platform")
}

func delRoute(Route) error {
	return errors.New("server routes are not supported on this platform")
}
