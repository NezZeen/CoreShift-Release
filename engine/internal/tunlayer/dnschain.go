package tunlayer

import (
	"fmt"
	"net/netip"
)

const (
	// tagDNSNetwork prefixes the tags of the network's own resolvers
	// (DNSOptions.DirectFallback): "network1", "network2".
	tagDNSNetwork = "network"
	// tagDNSLocal is the network's resolver for its own names
	// (DNSOptions.LocalResolver), when it is not the direct one.
	tagDNSLocal = "local"
	// singleLabelPattern matches a name without a dot, which only a local
	// network can know ("proxy", "nas"); wpadPattern the name browsers
	// look for a proxy under ("wpad", "wpad.corp.example").
	singleLabelPattern = `^[^.]+$`
	wpadPattern        = `^wpad(\.|$)`
	// maxDirectFallback is how many of the network's resolvers stand
	// behind the direct one: a router announces one or two.
	maxDirectFallback = 2
	// directAttemptTimeout bounds a lookup on the user's own direct
	// resolver when others stand behind it. The layer's usual timeout
	// (dnsTimeout) would leave a network that silently drops its packets
	// waiting almost as long as the system's own retry, which comes after
	// 5 s on Android and in glibc; this leaves time for the next resolver.
	// A connection refused or reset does not wait for it at all.
	directAttemptTimeout = "2s"
)

// directChain is the direct resolver and the network's resolvers behind
// it, tried in turn.
//
// sing-box has no fallback between DNS servers: a rule names one, and if
// it fails the lookup fails. What it has is "evaluate", which asks a server
// and keeps the answer under a tag, and "respond", which answers with what
// a tag holds, if there is one (match_response). A server that fails holds
// nothing, so the rules go on to the next evaluate. The next server is
// asked only then: an evaluate starts its lookup when the rules reach it,
// and a respond waits for the answer before the rules go on (unlike
// "race", which asks all at once, so every name would go to the network's
// resolver as well, to the provider that the user set their own DNS to be
// rid of).
//
// An answer is an answer, whatever its code: a name that does not exist
// does not exist on the next resolver either, and is not asked again.
// When none of them answers, the lookup fails with SERVFAIL, as a lookup
// on a dead resolver does, rather than falling through to the rules after
// (which would hand out a fake address, or ask yet another server).
type directChain struct {
	tags  []string     // the network's resolvers, in order
	addrs []netip.Addr // the same, as addresses
	n     int          // chains built, to give every evaluate its own tag
}

// any reports whether other resolvers stand behind the direct one.
func (c *directChain) any() bool { return len(c.tags) > 0 }

// rules are the DNS rules that send the names match selects (all, if
// empty) to the direct resolver and, if it does not answer, to the others.
func (c *directChain) rules(match obj) []any {
	with := func(r obj) obj {
		for k, v := range match {
			r[k] = v
		}
		return r
	}
	if !c.any() {
		return []any{with(obj{"server": tagDNSDirect})}
	}
	c.n++
	var rules []any
	for i, server := range append([]string{tagDNSDirect}, c.tags...) {
		tag := fmt.Sprintf("direct-%d-%d", c.n, i)
		evaluate := obj{"action": "evaluate", "server": server, "tag": tag}
		if i == 0 {
			evaluate["timeout"] = directAttemptTimeout
		}
		rules = append(rules,
			with(evaluate),
			obj{"action": "respond", "match_response": tag},
		)
	}
	return append(rules, with(obj{"action": "predefined", "rcode": "SERVFAIL"}))
}

// localResolver is DNSOptions.LocalResolver when it can be dialled: a
// link-local one needs an interface (%zone) that a plain address lacks.
func (o Options) localResolver() netip.Addr {
	a := o.DNS.LocalResolver
	if !a.IsValid() || (a.IsLinkLocalUnicast() && a.Zone() == "") {
		return netip.Addr{}
	}
	return a
}
