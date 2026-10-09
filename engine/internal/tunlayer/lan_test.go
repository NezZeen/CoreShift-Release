package tunlayer

import (
	"math/big"
	"net/netip"
	"slices"
	"testing"
)

func size(p netip.Prefix) *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), uint(p.Addr().BitLen()-p.Bits()))
}

// A resolver on the local network stays routed into the TUN, so DNS sent to
// it directly is hijacked; the rest of its range is still excluded.
func TestExcludeLANCarvesResolvers(t *testing.T) {
	resolvers := []netip.Addr{
		netip.MustParseAddr("172.23.64.1"),  // WSL's host
		netip.MustParseAddr("192.168.1.1"),  // a home router
		netip.MustParseAddr("192.168.1.1"),  // listed twice
		netip.MustParseAddr("fd12:3456::1"), // a ULA resolver
		netip.MustParseAddr("1.1.1.1"),      // public: routed anyway
		netip.MustParseAddr("127.0.0.53"),   // resolved's stub
		netip.MustParseAddr("fe80::1"),      // link-local: needs a zone
	}
	got := excludeLAN(resolvers)
	contains := func(a netip.Addr) bool {
		return slices.ContainsFunc(got, func(p netip.Prefix) bool { return p.Contains(a) })
	}
	for _, r := range resolvers[:4] {
		if contains(r) {
			t.Errorf("%s still excluded", r)
		}
	}
	for _, s := range []string{"172.23.64.0", "172.23.64.2", "172.16.0.1", "172.31.255.255", "192.168.1.2", "192.168.0.1",
		"10.1.2.3", "fd12:3456::2", "fd00::1", "fe80::1", "169.254.1.1", "224.0.0.251"} {
		if !contains(netip.MustParseAddr(s)) {
			t.Errorf("%s no longer excluded", s)
		}
	}
	// Exactly one address less in each carved range, and no overlaps.
	for _, c := range []struct {
		r      string
		minus1 bool
	}{{"172.16.0.0/12", true}, {"192.168.0.0/16", true}, {"fd00::/8", true}, {"10.0.0.0/8", false}, {"fe80::/10", false}} {
		r := netip.MustParsePrefix(c.r)
		sum := new(big.Int)
		for _, p := range got {
			if r.Overlaps(p) {
				if p.Bits() < r.Bits() {
					t.Fatalf("%s wider than %s", p, r)
				}
				sum.Add(sum, size(p))
			}
		}
		want := size(r)
		if c.minus1 {
			want.Sub(want, big.NewInt(1))
		}
		if sum.Cmp(want) != 0 {
			t.Errorf("%s: %s addresses excluded, want %s", r, sum, want)
		}
	}
	// Without resolvers on the local network: the ranges as they are.
	plain := excludeLAN(resolvers[4:])
	if len(plain) != len(lanRanges) {
		t.Fatalf("got %v", plain)
	}
	for i, p := range plain {
		if p.String() != lanRanges[i] {
			t.Errorf("%d: %s, want %s", i, p, lanRanges[i])
		}
	}
}

func TestBuildCarvesLANResolvers(t *testing.T) {
	o := Options{Upstream: netip.MustParseAddrPort("127.0.0.1:17890"), DNS: DNSOptions{Remote: "1.1.1.1", Direct: "192.168.1.1"},
		ExcludeLAN: true, LANResolvers: []netip.Addr{netip.MustParseAddr("192.168.1.1")}}
	cfg, err := build(o)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := cfg["inbounds"].([]any)[0].(obj)["route_exclude_address"].([]string)
	if slices.Contains(list, "192.168.0.0/16") || !slices.Contains(list, "192.168.1.0/32") || !slices.Contains(list, "10.0.0.0/8") {
		t.Errorf("route_exclude_address: %v", list)
	}
}

// A router's IPv6 link-local resolver, with its zone or without: never
// carved out of the local ranges (it cannot be routed into the TUN), and
// never a direct resolver dialled without its interface.
func TestLinkLocalResolver(t *testing.T) {
	base := func() Options {
		return Options{Upstream: netip.MustParseAddrPort("127.0.0.1:17890"), DNS: DNSOptions{Remote: "1.1.1.1", Direct: "192.168.1.1"},
			Address6: DefaultAddress6, ExcludeLAN: true}
	}
	o := base()
	o.LANResolvers = []netip.Addr{netip.MustParseAddr("fe80::52ff:20ff:feb4:407b"), netip.MustParseAddr("fe80::1%wlan0"), netip.MustParseAddr("192.168.1.1")}
	cfg, err := build(o)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := cfg["inbounds"].([]any)[0].(obj)["route_exclude_address"].([]string)
	if !slices.Contains(list, "fe80::/10") {
		t.Errorf("route_exclude_address: %v", list)
	}
	for _, s := range cfg["dns"].(obj)["servers"].([]any) {
		if srv, _ := s.(obj)["server"].(string); srv != "" {
			if a, err := netip.ParseAddr(srv); err == nil && a.IsLinkLocalUnicast() {
				t.Errorf("link-local DNS server %v", s)
			}
		}
	}

	o = base()
	for _, d := range []string{"fe80::52ff:20ff:feb4:407b", "[fe80::52ff:20ff:feb4:407b]:53", "udp://[fe80::1]:53"} {
		o.DNS.Direct = d
		if _, err := Build(o); err == nil {
			t.Errorf("zone-less link-local direct resolver %s accepted", d)
		}
	}
	o.DNS.Direct = "fe80::1%wlan0"
	cfg, err = build(o)
	if err != nil {
		t.Fatal(err)
	}
	if srv := cfg["dns"].(obj)["servers"].([]any)[1].(obj)["server"]; srv != "fe80::1%wlan0" {
		t.Errorf("direct server = %v, want it with its zone", srv)
	}
}
