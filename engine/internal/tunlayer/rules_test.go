package tunlayer

import (
	"slices"
	"testing"
)

// What "ip rule show" printed after the daemon was killed with its TUN
// layer up (Fedora 44), with the rule priorities moved to ours, plus rules
// of others.
func TestStaleRulePrefs(t *testing.T) {
	out := []byte(`0:	from all lookup local
5210:	from all fwmark 0x80000/0xff0000 lookup main
9000:	from all lookup 2022 suppress_prefixlength 0
17900:	from all to 172.19.0.0/30 lookup 17900
17901:	from all lookup 17900 suppress_prefixlength 0
17902:	not from all dport 53 lookup main suppress_prefixlength 0
17902:	from all iif coreshift [detached] goto 17910
17903:	not from all iif lo lookup 17900
17903:	from 0.0.0.0 iif lo lookup 17900
17910:	from all nop
17950:	from all lookup 100
32766:	from all lookup main
32767:	from all lookup default
`)
	want := []int{17900, 17901, 17902, 17902, 17903, 17903, 17910}
	if got := staleRulePrefs(out); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := staleRulePrefs(nil); len(got) != 0 {
		t.Fatalf("empty output: %v", got)
	}
}

func TestBuildUsesOwnRouteTable(t *testing.T) {
	cfg, err := build(baseOptions())
	if err != nil {
		t.Fatal(err)
	}
	tun := cfg["inbounds"].([]any)[0].(obj)
	if tun["iproute2_table_index"] != RouteTable || tun["iproute2_rule_index"] != RuleIndex {
		t.Fatalf("tun = %v", tun)
	}
}
