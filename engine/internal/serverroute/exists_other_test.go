//go:build !windows && !linux

package serverroute

func routeExists(Route) bool { return false }
