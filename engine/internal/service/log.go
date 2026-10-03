package service

import (
	"strings"
	"time"
)

// logGroupEvery is how often a repeating error is reported (see logGrouper).
const logGroupEvery = 30 * time.Second

// Log adds a line of output to the event stream, as the TUN layer reports
// it (a separate process on the desktop, part of the app on Android).
func (s *Service) Log(source, line string) {
	switch {
	case noiseLine(line):
	case s.healthFails.Load() >= upstreamDeadChecks && lookupTimeout(line):
		// Every app's lookups through the tunnel time out while the
		// server does not answer: one line says it, not one per name.
		s.logs.addAs(source, upstreamDNSDead)
	default:
		s.logs.add(source, line)
	}
}

// upstreamDeadChecks failed checks in a row of the active core mean the
// tunnel's own DNS cannot answer either.
const upstreamDeadChecks = 2

const upstreamDNSDead = "DNS через VPN не отвечает: сервер недоступен"

// lookupTimeout recognises the TUN layer's lookup that timed out, as every
// lookup through a tunnel whose server is gone does.
func lookupTimeout(l string) bool {
	return strings.Contains(l, "lookup failed") &&
		(strings.Contains(l, "context deadline exceeded") || strings.Contains(l, "i/o timeout"))
}

// noiseLine recognises TUN layer errors that are no fault of the tunnel,
// yet read like the VPN breaking:
//   - a name that does not exist (NXDOMAIN, often an ad or tracker host):
//     the layer resolves names to match addresses against geoip, and
//     reports every such failure;
//   - a connection the app on the device closed first ("endpoint not
//     connected"), and a handshake report to an app that had gone: on
//     Android many a minute, as apps drop idle connections.
func noiseLine(l string) bool {
	switch {
	case strings.Contains(l, "NXDOMAIN"):
		return strings.Contains(l, "dns: lookup failed") || strings.Contains(l, "router: lookup")
	case strings.Contains(l, "endpoint not connected"):
		return strings.Contains(l, "connection download closed") || strings.Contains(l, "connection upload closed")
	}
	return strings.Contains(l, "report handshake success")
}
