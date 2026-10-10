package supervisor

import (
	"slices"
	"time"

	"coreshift/engine/internal/core"
)

// Policy is the part of Config the user can change between connections.
type Policy struct {
	Priority             []core.Kind
	Mode                 Mode
	ManualCore           core.Kind
	Health               Health
	ReturnToPrimaryAfter time.Duration
	Fragment             bool
	OpenInbound          bool // Config.OpenInbound
	Inbound              Inbound
	// LogLevel is the cores' (Config.LogLevel); "" is the default, warn.
	LogLevel string
}

// SetPolicy replaces the swap policy. It stops any running connection, so
// the caller reconnects to apply it.
func (s *Supervisor) SetPolicy(p Policy) {
	s.Disconnect()
	s.mu.Lock() // TestLatency reads the config concurrently
	defer s.mu.Unlock()
	c := s.cfg
	c.Priority, c.Mode, c.ManualCore = slices.Clone(p.Priority), p.Mode, p.ManualCore
	c.Health, c.ReturnToPrimaryAfter, c.Fragment = p.Health, p.ReturnToPrimaryAfter, p.Fragment
	c.OpenInbound, c.Inbound = p.OpenInbound, p.Inbound
	c.LogLevel = p.LogLevel
	s.cfg = c.withDefaults()
}
