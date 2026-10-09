package service

import (
	"strings"
	"time"

	"coreshift/engine/internal/supervisor"
)

// logGroupEvery is how often a repeating error is reported (see logGrouper).
const logGroupEvery = 30 * time.Second

// Log adds a line of output to the event stream, as the TUN layer reports
// it (a separate process on the desktop, part of the app on Android), and
// as the cores print it (onCoreEvent).
func (s *Service) Log(source, line string) {
	// The TUN layer's and the cores' complaints that the device has no
	// default interface: the network watcher looks at once (netwatch.go).
	if netHint(line) {
		s.kickNetwork()
	}
	line = tidy(line)
	switch {
	case noiseLine(line):
	case s.logs.twin(line):
		// sing-box's second report of a lookup it just reported.
	case s.healthFails.Load() >= upstreamDeadChecks && lookupTimeout(line):
		// Every app's lookups through the tunnel time out while the
		// server does not answer: one line says it, not one per name.
		s.logs.addAs(source, upstreamDNSDead)
	default:
		s.logs.addTidy(source, line)
	}
}

// onCoreEvent is the supervisor's event handler: the cores' output goes
// through the journal's grouper as the TUN layer's does, so that a server
// out of reach is one line and a count rather than a line for every
// connection of every app and every health check; the rest goes on as it
// is (onSupervisorEvent).
func (s *Service) onCoreEvent(e supervisor.Event) {
	if e.Kind == supervisor.EventLog {
		s.Log(string(e.Core), e.Line)
		return
	}
	s.onSupervisorEvent(e)
}

// upstreamDeadChecks failed checks in a row of the active core mean the
// tunnel's own DNS cannot answer either.
const upstreamDeadChecks = 2

const upstreamDNSDead = "DNS через VPN не отвечает: сервер недоступен"

// lookupTimeout recognises the TUN layer's lookup that timed out, as every
// lookup through a tunnel whose server is gone does.
func lookupTimeout(l string) bool {
	return (strings.Contains(l, "lookup failed") || strings.Contains(l, "router: lookup")) &&
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
//
// and the cores' remarks on every start that say nothing of the connection:
//   - sing-box on Android reads the system's list of apps for rules by app,
//     which CoreShift does not give it and an app may not read; no setting
//     turns the attempt off outside the platform's own VPN service;
//   - mihomo names the geodata loader and matcher it uses, as it reads the
//     config, before the log level in it applies.
func noiseLine(l string) bool {
	switch {
	case strings.Contains(l, "NXDOMAIN"):
		return strings.Contains(l, "dns: lookup failed") || strings.Contains(l, "router: lookup")
	case strings.Contains(l, "endpoint not connected"):
		return strings.Contains(l, "connection download closed") || strings.Contains(l, "connection upload closed")
	case strings.Contains(l, "initialize package manager"):
		return strings.Contains(l, "/data/system/packages.xml: permission denied")
	case strings.HasPrefix(l, "INFO Geodata Loader mode: "), strings.HasPrefix(l, "INFO Geosite Matcher implementation: "):
		return true
	}
	return strings.Contains(l, "report handshake success")
}
