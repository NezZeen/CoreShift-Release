package tunlayer

import "net/netip"

// excludeLAN returns lanRanges without the resolvers that lie in them (see
// Options.LANResolvers): a range holding one is split into the prefixes
// around it, so only that address stays routed into the TUN.
func excludeLAN(resolvers []netip.Addr) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(lanRanges))
	for _, r := range lanRanges {
		out = append(out, netip.MustParsePrefix(r))
	}
	for _, a := range resolvers {
		a = a.WithZone("").Unmap()
		if !a.IsPrivate() {
			continue
		}
		var next []netip.Prefix
		for _, p := range out {
			next = append(next, carve(p, a)...)
		}
		out = next
	}
	return out
}

// carve returns p without a: p itself when a is outside it, else the
// sibling prefixes along the way from p down to a's single address.
func carve(p netip.Prefix, a netip.Addr) []netip.Prefix {
	if !p.Contains(a) {
		return []netip.Prefix{p}
	}
	var out []netip.Prefix
	for b := p.Bits(); b < a.BitLen(); b++ {
		out = append(out, netip.PrefixFrom(flipBit(a, b), b+1).Masked())
	}
	return out
}

// flipBit flips bit i of a, counted from the most significant.
func flipBit(a netip.Addr, i int) netip.Addr {
	if a.Is4() {
		b := a.As4()
		b[i/8] ^= 0x80 >> (i % 8)
		return netip.AddrFrom4(b)
	}
	b := a.As16()
	b[i/8] ^= 0x80 >> (i % 8)
	return netip.AddrFrom16(b)
}
