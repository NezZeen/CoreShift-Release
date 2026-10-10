package supervisor

import (
	"fmt"
	"strings"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
)

func (s *Supervisor) chain(n *node.Node) ([]core.Kind, error) { return s.cfg.chain(n) }

// chain returns the installed cores able to run n, in priority order.
func (c Config) chain(n *node.Node) ([]core.Kind, error) {
	var installed []core.Kind
	for _, k := range c.Priority {
		if c.Binaries[k] != "" {
			installed = append(installed, k)
		}
	}
	if c.Mode == Manual {
		k := c.ManualCore
		a, ok := core.ByKind(k)
		if !ok || c.Binaries[k] == "" {
			return nil, fmt.Errorf("%w: %q is not installed", ErrNoCore, k)
		}
		if err := a.Supports(n); err != nil {
			return nil, err
		}
		return []core.Kind{k}, nil
	}
	chain := core.Compatible(n, installed)
	if len(chain) == 0 {
		var why []string
		for _, k := range installed {
			a, _ := core.ByKind(k)
			why = append(why, a.Supports(n).Error())
		}
		return nil, fmt.Errorf("%w (%s)", ErrNoCore, strings.Join(why, "; "))
	}
	return chain, nil
}
