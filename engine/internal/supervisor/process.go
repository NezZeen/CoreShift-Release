package supervisor

import (
	"net/netip"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/proc"
)

// process is one running core.
type process struct {
	*proc.Process
	kind   core.Kind
	listen netip.AddrPort
	probe  bool
	// stats is where the core reports traffic (see core.ReadTraffic);
	// invalid for probes.
	stats  netip.AddrPort
	secret string
}

func (p *process) stop() {
	if p != nil {
		p.Process.Stop()
	}
}
