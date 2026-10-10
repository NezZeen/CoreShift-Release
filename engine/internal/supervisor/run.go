package supervisor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
)

func (s *Supervisor) run(ctx context.Context, n node.Node, serverAddr string, chain []core.Kind, ready chan<- error) {
	signal := func(err error) {
		if ready != nil {
			ready <- err
			ready = nil
		}
	}
	// restartDone answers a Restart under way, if any.
	restartDone := func(err error) {
		if s.restarted != nil {
			s.restarted <- err
			s.restarted = nil
		}
	}
	defer func() { restartDone(ErrNotConnected) }()
	failed := map[core.Kind]error{}
	restarted := map[core.Kind]time.Time{} // the last restart of a hung core
	var prev core.Kind
	var prevReason Reason
	// next is a core already seen working on the spare port.
	var next core.Kind
	for {
		k, ok := next, next != ""
		next = ""
		if !ok {
			k, ok = firstNotFailed(chain, failed)
		}
		if !ok {
			err := fmt.Errorf("%w: %s", ErrChainExhausted, describe(chain, failed))
			s.setState(Failed, "")
			signal(err)
			return
		}
		if prev == "" {
			s.setState(Connecting, k)
		} else {
			s.setState(Swapping, k)
		}

		p, err := s.launch(ctx, k, n, serverAddr, false)
		if ctx.Err() != nil {
			p.stop()
			s.setState(Idle, "")
			signal(ctx.Err())
			return
		}
		if err != nil {
			p.stop()
			s.drop(failed, k, ReasonStartFailed, err)
			restartDone(err)
			prev, prevReason = k, ReasonStartFailed
			continue
		}

		if prev != "" && prev != k {
			s.emit(Event{Kind: EventSwap, Core: k, From: prev, Reason: prevReason})
		}
		s.serve(p)
		s.setState(Connected, k)
		signal(nil)
		restartDone(nil)

		reason, alt, err := s.monitor(ctx, p, n, serverAddr, chain, failed)
		// Connections to it are closed before it stops: their clients learn
		// at once, and new ones wait for the next core.
		s.serve(nil)
		p.stop()
		switch reason {
		case "":
			s.setState(Idle, "")
			return
		case ReasonReturn:
			clear(failed)
			s.mu.Lock()
			clear(s.status.Failed)
			s.mu.Unlock()
		case ReasonHealth:
			s.drop(failed, k, reason, err)
			next = alt
		case ReasonRestart:
			next = k
		case ReasonHung:
			// A hung process is restarted once; hanging again soon after
			// means the core itself is the trouble, and it is dropped.
			if time.Since(restarted[k]) > hungRestartWindow {
				restarted[k] = time.Now()
				s.emit(Event{Kind: EventRestart, Core: k, Reason: reason, Err: err})
				next = k
			} else {
				s.drop(failed, k, reason, err)
			}
		default:
			s.drop(failed, k, reason, err)
		}
		prev, prevReason = k, reason
	}
}

func firstNotFailed(chain []core.Kind, failed map[core.Kind]error) (core.Kind, bool) {
	for _, k := range chain {
		if _, bad := failed[k]; !bad {
			return k, true
		}
	}
	return "", false
}

func describe(chain []core.Kind, failed map[core.Kind]error) string {
	parts := make([]string, 0, len(chain))
	for _, k := range chain {
		parts = append(parts, fmt.Sprintf("%s: %v", k, failed[k]))
	}
	return strings.Join(parts, "; ")
}
