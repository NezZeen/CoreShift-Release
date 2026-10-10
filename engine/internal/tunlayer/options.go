package tunlayer

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
)

func (o Options) withDefaults() Options {
	if o.Platform {
		o.BypassProcesses, o.DirectDNSProcesses, o.DirectApps, o.ProxyApps = nil, nil, nil, nil
		o.BypassAddresses = nil
		o.ExcludeLAN, o.LANResolvers = false, nil
		o.RefuseIPv6 = false
		// The system stack answers TCP from a kernel socket of this process,
		// which Android keeps outside its own VPN: the answers leave by the
		// physical network and every TCP connection hangs. gVisor answers
		// through the TUN itself.
		o.Stack = "gvisor"
	}
	if o.InterfaceName == "" {
		o.InterfaceName = DefaultInterface
	}
	if o.RefuseIPv6 {
		if !o.Address6.IsValid() {
			o.Address6 = DefaultAddress6
		}
		o.DNS.DirectIPv4Only = false // nothing IPv6 goes direct anyway
	}
	if !o.Address.IsValid() {
		o.Address = DefaultAddress
	}
	if o.MTU == 0 {
		o.MTU = DefaultMTU
	}
	if o.Stack == "" {
		o.Stack = DefaultStack
	}
	if o.LogLevel == "" {
		o.LogLevel = "warn"
	}
	if o.DNS.FakeIP && !o.DNS.FakeIPRange.IsValid() {
		o.DNS.FakeIPRange = DefaultFakeIPRange
	}
	if o.DNS.FakeIP && o.ipv6() && !o.DNS.FakeIPRange6.IsValid() {
		o.DNS.FakeIPRange6 = DefaultFakeIPRange6
	}
	if !o.ipv6() {
		o.DNS.FakeIPRange6 = netip.Prefix{}
	}
	return o
}

// ipv6 reports whether IPv6 goes through the tunnel: it has an IPv6
// address, and IPv6 is not refused there.
func (o Options) ipv6() bool { return o.Address6.IsValid() && !o.RefuseIPv6 }

func (o Options) validate() error {
	if !o.Upstream.IsValid() || o.Upstream.Port() == 0 {
		return errors.New("tunlayer: upstream SOCKS address is required")
	}
	if !o.Address.Addr().Is4() {
		return errors.New("tunlayer: Address must be an IPv4 prefix")
	}
	if o.Address6.IsValid() && !o.Address6.Addr().Is6() {
		return errors.New("tunlayer: Address6 must be an IPv6 prefix")
	}
	if !slices.Contains([]string{"mixed", "system", "gvisor"}, o.Stack) {
		return fmt.Errorf("tunlayer: unknown stack %q", o.Stack)
	}
	if o.DNS.Remote == "" {
		return errors.New("tunlayer: remote DNS server is required")
	}
	if o.DNS.Direct == "" {
		return errors.New("tunlayer: direct DNS server is required")
	}
	for _, r := range o.Rules {
		if err := r.validate(); err != nil {
			return err
		}
	}
	seen := map[string]RuleSet{}
	for _, rs := range o.ruleSets(true) {
		if rs.Tag == "" || (rs.Path == "") == (rs.URL == "") {
			return errors.New("tunlayer: rule set needs a tag and either a path or a URL")
		}
		if prev, ok := seen[rs.Tag]; ok && prev != rs {
			return fmt.Errorf("tunlayer: two rule sets tagged %q", rs.Tag)
		}
		seen[rs.Tag] = rs
	}
	return nil
}

// ruleSets returns the rule sets the configuration refers to, or with all
// every one it was given; Selective mode never refers to the direct ones.
// One used twice (by a preset and by a rule) is in the list twice.
func (o Options) ruleSets(all bool) []RuleSet {
	sets := slices.Concat(o.DNS.BlockRuleSets, o.DNS.PinnedRuleSets, o.DNS.ProxyRuleSets)
	if all || !o.Selective {
		sets = slices.Concat(sets, o.DNS.DirectRuleSets, o.DNS.DirectIPRuleSets)
	}
	for _, r := range o.Rules {
		if r.Set != nil {
			sets = append(sets, *r.Set)
		}
	}
	return sets
}
