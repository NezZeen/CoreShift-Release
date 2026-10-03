package service

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestResolverRuleArgs(t *testing.T) {
	got := resolverRuleArgs("add", []netip.Addr{
		netip.MustParseAddr("172.31.48.1"),
		netip.MustParseAddr("::ffff:172.31.48.1"), // the same, mapped
		netip.MustParseAddr("1.1.1.1"),            // public: not carved out
		netip.MustParseAddr("127.0.0.53"),         // loopback
		netip.MustParseAddr("fe80::1%eth0"),       // link-local
		netip.MustParseAddr("fd00::53"),
	})
	var lines []string
	for _, a := range got {
		lines = append(lines, strings.Join(a, " "))
	}
	want := []string{
		"ip -4 rule add priority 8998 to 172.31.48.1/32 ipproto udp dport 53 lookup 2022",
		"ip -4 rule add priority 8998 to 172.31.48.1/32 ipproto tcp dport 53 lookup 2022",
		"ip -4 rule add priority 8999 to 172.31.48.1/32 lookup main",
		"ip -6 rule add priority 8998 to fd00::53/128 ipproto udp dport 53 lookup 2022",
		"ip -6 rule add priority 8998 to fd00::53/128 ipproto tcp dport 53 lookup 2022",
		"ip -6 rule add priority 8999 to fd00::53/128 lookup main",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if n := len(resolverRuleArgs("del", []netip.Addr{netip.MustParseAddr("8.8.8.8")})); n != 0 {
		t.Fatalf("public resolver: %d rules", n)
	}
}

// The rules must sit ahead of sing-box's own, which start at 9000.
func TestResolverRulesBeforeSingBox(t *testing.T) {
	if resolverDNSPriority >= resolverRestPriority || resolverRestPriority >= 9000 {
		t.Fatalf("priorities %d, %d", resolverDNSPriority, resolverRestPriority)
	}
}
