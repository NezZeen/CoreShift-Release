package supervisor

import (
	"maps"
	"slices"
	"time"

	"coreshift/engine/internal/core"
)

func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Chain = slices.Clone(st.Chain)
	st.Failed = maps.Clone(st.Failed)
	return st
}

// Traffic returns the byte counters of the connection, which the SOCKS
// port counts whatever core runs behind it, and which connection they
// belong to: they restart from zero whenever run changes.
func (s *Supervisor) Traffic() (t core.Traffic, run int, err error) {
	s.mu.Lock()
	gate, run := s.gate, s.gates
	s.mu.Unlock()
	if gate == nil {
		return core.Traffic{}, run, ErrNotConnected
	}
	return gate.Traffic(), run, nil
}

func (s *Supervisor) drop(failed map[core.Kind]error, k core.Kind, reason Reason, err error) {
	failed[k] = err
	s.mu.Lock()
	s.status.Failed[k] = err.Error()
	s.mu.Unlock()
	s.emit(Event{Kind: EventCoreFailed, Core: k, Reason: reason, Err: err})
}

func (s *Supervisor) setState(st State, k core.Kind) {
	s.mu.Lock()
	s.status.State, s.status.Core = st, k
	s.mu.Unlock()
	s.emit(Event{Kind: EventState, State: st, Core: k})
}

func (s *Supervisor) emit(e Event) {
	if s.cfg.OnEvent != nil {
		e.Time = time.Now()
		s.cfg.OnEvent(e)
	}
}
