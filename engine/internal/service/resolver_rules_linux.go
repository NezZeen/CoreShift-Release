package service

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

// addResolverRules installs the rules of resolverRuleArgs and returns what
// removes them. Rules a crashed daemon left behind are removed first; they
// are harmless meanwhile (sing-box's table is gone, so lookups fall through
// to the main table) but would pile up.
func addResolverRules(resolvers []netip.Addr, log func(string)) func() {
	if len(resolverRuleArgs("add", resolvers)) == 0 {
		return func() {}
	}
	del := func() {
		for _, args := range resolverRuleArgs("del", resolvers) {
			// One per run; a few runs clear duplicates.
			for i := 0; i < 4; i++ {
				if exec.Command(args[0], args[1:]...).Run() != nil {
					break
				}
			}
		}
	}
	del()
	for _, args := range resolverRuleArgs("add", resolvers) {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil && log != nil {
			log(fmt.Sprintf("%s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out))))
		}
	}
	return del
}
