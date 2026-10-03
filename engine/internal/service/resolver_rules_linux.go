package service

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
)

// removeStaleResolverRules deletes every rule at the resolver priorities:
// a daemon killed while connected (kill -9, a crash) cannot remove its
// own, and the resolvers it used may differ from the next connection's.
// Called once at start (Service.Recover), when no TUN of ours runs.
func removeStaleResolverRules() {
	for _, fam := range []string{"-4", "-6"} {
		for _, prio := range []int{resolverDNSPriority, resolverRestPriority} {
			// One rule per run; a bound in case ip keeps succeeding.
			for i := 0; i < 64; i++ {
				if exec.Command("ip", fam, "rule", "del", "priority", strconv.Itoa(prio)).Run() != nil {
					break
				}
			}
		}
	}
}

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
