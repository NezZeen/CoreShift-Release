// Package proc runs the helper processes the engine depends on (proxy cores
// and the TUN layer): their output arrives line by line, the last lines are
// kept to explain a crash, and they are tied to the daemon's lifetime so a
// crashed daemon never leaves them holding ports or the TUN interface.
package proc

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Group ties processes to the daemon: on Windows a job object kills them when
// the daemon exits for any reason; on Linux and Android Pdeathsig does.
type Group struct {
	g *processGroup
}

func NewGroup() (*Group, error) {
	g, err := newProcessGroup()
	if err != nil {
		return nil, err
	}
	return &Group{g: g}, nil
}

type Spec struct {
	Name string // used in error messages
	Path string
	Args []string
	Dir  string
	// OnLine receives each line of stdout and stderr, from one goroutine.
	OnLine func(string)
	// Graceful makes Stop ask the process to exit (CTRL_BREAK on Windows,
	// SIGTERM elsewhere) before killing it, for processes that must clean
	// up after themselves, like the TUN layer removing its adapter.
	Graceful bool
}

type Process struct {
	name     string
	graceful bool
	cmd      *exec.Cmd
	tail     *tail
	exited   chan struct{}
	err      error // exit status; valid once exited is closed
}

// Start launches spec. Failing to tie the process to the group is reported
// through OnLine rather than failing the start.
func (g *Group) Start(spec Spec) (*Process, error) {
	p := &Process{name: spec.Name, graceful: spec.Graceful, tail: &tail{max: 20}, exited: make(chan struct{})}
	w := &lineWriter{onLine: func(line string) {
		p.tail.add(line)
		if spec.OnLine != nil {
			spec.OnLine(line)
		}
	}}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdout, cmd.Stderr = w, w
	prepareCmd(cmd, spec.Graceful)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.Name, err)
	}
	p.cmd = cmd
	if err := g.g.add(cmd.Process); err != nil {
		w.onLine("warning: process is not tied to the daemon's lifetime: " + err.Error())
	}
	go func() {
		p.err = cmd.Wait()
		w.flush()
		close(p.exited)
	}()
	return p, nil
}

// Exited is closed once the process has exited.
func (p *Process) Exited() <-chan struct{} { return p.exited }

// ExitError describes the exit, quoting the last output lines, which usually
// say why (e.g. a rejected config). Call it after Exited is closed.
func (p *Process) ExitError() error {
	msg := p.name + " exited"
	if p.err != nil {
		msg += " (" + p.err.Error() + ")"
	}
	if last := p.tail.last(3); last != "" {
		msg += ": " + last
	}
	return fmt.Errorf("%s", msg)
}

// gracefulTimeout is how long a graceful process gets to exit by itself.
const gracefulTimeout = 5 * time.Second

// Stop ends the process and waits for it to go: politely first for a
// graceful one, otherwise, or if that does not work, by killing it.
func (p *Process) Stop() {
	if p == nil {
		return
	}
	select {
	case <-p.exited:
		return
	default:
	}
	if p.graceful && interrupt(p.cmd.Process) == nil {
		select {
		case <-p.exited:
			return
		case <-time.After(gracefulTimeout):
		}
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.exited:
	case <-time.After(5 * time.Second):
	}
}

// WaitFor polls ready until it returns true, the process exits or timeout.
func (p *Process) WaitFor(ctx context.Context, timeout time.Duration, what string, ready func() bool) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-p.exited:
			return p.ExitError()
		case <-deadline.C:
			return fmt.Errorf("%s: %s not ready within %s", p.name, what, timeout)
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if ready() {
				return nil
			}
		}
	}
}

// PortOpen reports whether something accepts TCP connections on addr.
func PortOpen(addr netip.AddrPort) bool {
	c, err := net.DialTimeout("tcp", addr.String(), 200*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

type tail struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func (t *tail) add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *tail) last(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines[max(0, len(t.lines)-n):], " | ")
}

// lineWriter splits process output into lines. exec.Cmd serialises writes
// when Stdout and Stderr are the same writer.
type lineWriter struct {
	onLine func(string)
	buf    []byte
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 64<<10 { // a runaway line without newline
		w.flush()
	}
	return len(b), nil
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

func (w *lineWriter) emit(line []byte) {
	if s := strings.TrimSpace(stripANSI(string(line))); s != "" {
		w.onLine(s)
	}
}

// stripANSI removes color codes some cores print even without a terminal.
func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
