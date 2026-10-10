package tunlayer

import (
	"errors"
	"fmt"
	"slices"
)

func (r Rule) validate() error {
	n := 0
	for _, has := range []bool{len(r.Domains) > 0, len(r.IPs) > 0, r.Set != nil} {
		if has {
			n++
		}
	}
	if n != 1 {
		return errors.New("tunlayer: a rule needs exactly one of domains, addresses or a rule set")
	}
	if !slices.Contains([]string{ActionProxy, ActionDirect, ActionBlock}, r.Action) {
		return fmt.Errorf("tunlayer: unknown rule action %q", r.Action)
	}
	if r.SetIP && r.Set == nil {
		return errors.New("tunlayer: an address rule set rule without a rule set")
	}
	return nil
}

// byName reports whether r is decided by the name or the address a
// connection has: every rule but one of an address set, which needs the
// address of a name looked up first.
func (r Rule) byName() bool { return !r.SetIP }

// match returns the condition of r for route rules, and with dns for DNS
// rules (nil for rules that DNS cannot decide: addresses).
func (r Rule) match(dns bool) obj {
	switch {
	case len(r.Domains) > 0:
		return obj{"domain_suffix": r.Domains}
	case r.Set != nil && (!dns || !r.SetIP):
		return obj{"rule_set": []string{r.Set.Tag}}
	case len(r.IPs) > 0 && !dns:
		return obj{"ip_cidr": prefixStrings(r.IPs)}
	}
	return nil
}

// routeRule returns the route rule of r.
func (r Rule) routeRule() obj {
	m := r.match(false)
	switch r.Action {
	case ActionBlock:
		m["action"] = "reject"
	case ActionDirect:
		m["outbound"] = tagDirect
	default:
		m["outbound"] = tagProxy
	}
	return m
}
