//go:build !windows && !linux

package ping

func anyDefaultRoute([]string) (bool, error) { return false, ErrUnsupported }
