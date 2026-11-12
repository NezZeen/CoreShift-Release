package supervisor

import (
	"context"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/proc"
)

// process is one running core.
type process struct {
	*proc.Process
	kind   core.Kind
	listen netip.AddrPort
	// auth is what its SOCKS inbound requires, made for this start.
	auth  core.SOCKSAuth
	probe bool
	// portLost is closed when the core reports that it could not open its
	// SOCKS port (portWatch).
	portLost <-chan struct{}
	// started is when its executable was started (Restart).
	started time.Time
	// config is the file the core was started with: it holds the node's
	// credentials and goes with the process.
	config string
}

func (p *process) stop() {
	if p != nil {
		p.Process.Stop()
		// Stops are sequential per core directory (a new start comes only
		// after the previous process was stopped), so this never removes the
		// config of its successor.
		_ = os.Remove(p.config)
	}
}

// lost returns the channel closed when the core could not open its port;
// nil, which never fires, when nothing watches.
func (p *process) lost() <-chan struct{} {
	if p == nil {
		return nil
	}
	return p.portLost
}

// portWatch notices a core saying that its SOCKS port at addr is taken.
// Xray and sing-box exit then, but mihomo keeps running without its
// listener: whatever else holds the port would answer in its place, and
// the TUN layer would send the device's traffic there. The port answering
// is therefore no proof that the core listens; this is the core's own word
// that it does not.
type portWatch struct {
	needle string
	once   sync.Once
	ch     chan struct{}
}

func newPortWatch(addr netip.AddrPort) *portWatch {
	// "listen tcp 127.0.0.1:17890: bind: …" in all three cores' errors.
	return &portWatch{needle: "listen tcp " + addr.String() + ": bind", ch: make(chan struct{})}
}

// line checks one line of the core's output.
func (w *portWatch) line(l string) {
	if strings.Contains(l, w.needle) {
		w.once.Do(func() { close(w.ch) })
	}
}

// settle waits up to d for the core to say its port is taken and reports
// whether it stayed silent. A cancelled ctx counts as silent: the caller
// sees the cancellation itself.
func (w *portWatch) settle(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-w.ch:
		return false
	case <-t.C:
	case <-ctx.Done():
	}
	return !w.failed()
}

// failed reports whether the core said its port is taken.
func (w *portWatch) failed() bool {
	select {
	case <-w.ch:
		return true
	default:
		return false
	}
}
