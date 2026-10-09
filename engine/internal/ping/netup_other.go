//go:build !windows && !linux

package ping

func defaultRouteInterface([]string) (string, error) { return "", ErrUnsupported }
