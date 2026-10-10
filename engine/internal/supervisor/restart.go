package supervisor

import (
	"context"
	"errors"
	"time"

	"coreshift/engine/internal/core"
)

// ReturnToPrimary moves back to the primary core now instead of waiting for
// ReturnToPrimaryAfter. The primary is tried on the spare port first; if it
// does not work, the current core keeps running and the error says why.
func (s *Supervisor) ReturnToPrimary(ctx context.Context) error {
	s.mu.Lock()
	running := s.cancel != nil
	s.mu.Unlock()
	if !running {
		return ErrNotConnected
	}
	reply := make(chan error, 1)
	select {
	case s.returnReq <- reply:
	case <-time.After(3 * time.Second):
		return errors.New("the core is switching right now; try again in a moment")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type restartRequest struct {
	kind   core.Kind
	before time.Time
	reply  chan error
}

// Restart starts core k again, when it is the one running and started
// before the given time: an update replaced its executable since, and only
// a new start runs the new one. The new start is tried on a spare port
// first, so a version that does not work here leaves the running core as
// it is (and the error says why); then it takes over as after a swap,
// connections arriving meanwhile waiting for it. Restart returns once it
// serves. ErrNotNeeded means there is nothing to restart.
func (s *Supervisor) Restart(ctx context.Context, k core.Kind, before time.Time) error {
	s.mu.Lock()
	running := s.cancel != nil
	s.mu.Unlock()
	if !running {
		return ErrNotConnected
	}
	req := restartRequest{kind: k, before: before, reply: make(chan error, 1)}
	select {
	case s.restartReq <- req:
	case <-time.After(3 * time.Second):
		return errors.New("the core is switching right now; try again in a moment")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
