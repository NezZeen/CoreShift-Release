package supervisor

import (
	"net/netip"
	"strings"
	"sync"

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
}

func (p *process) stop() {
	if p != nil {
		p.Process.Stop()
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

// failed reports whether the core said its port is taken.
func (w *portWatch) failed() bool {
	select {
	case <-w.ch:
		return true
	default:
		return false
	}
}
