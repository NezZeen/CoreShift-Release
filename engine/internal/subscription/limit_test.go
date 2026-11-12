package subscription

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseKeepsAtMostMaxNodes(t *testing.T) {
	var b strings.Builder
	total := MaxNodes + 250
	for i := range total {
		fmt.Fprintf(&b, "trojan://pw%d@203.0.113.5:443?sni=a.example.com#n%d\n", i, i)
	}
	res, err := Parse([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != MaxNodes {
		t.Fatalf("kept %d nodes, want %d", len(res.Nodes), MaxNodes)
	}
	if res.Nodes[MaxNodes-1].Name != fmt.Sprintf("n%d", MaxNodes-1) {
		t.Errorf("not the first ones kept: last is %s", res.Nodes[MaxNodes-1].Name)
	}
	last := res.Skipped[len(res.Skipped)-1]
	if last.Kind != "limit" || !strings.Contains(last.Reason, "250 left out") {
		t.Errorf("skipped = %v", last)
	}
}

func TestLimitNodesKeepsAutoOfKeptNodes(t *testing.T) {
	res := mustParse(t, []byte(linkList))
	for i := range res.Nodes {
		res.Auto = append(res.Auto, res.Nodes[i].Fingerprint())
	}
	limitNodes(&res, 2)
	if len(res.Nodes) != 2 || len(res.Auto) != 2 || res.Auto[1] != res.Nodes[1].Fingerprint() {
		t.Errorf("nodes %d, auto %v", len(res.Nodes), res.Auto)
	}
}

// A list of junk lines makes one skipped entry per line: only the first are
// kept, the rest is counted, and what is kept is short.
func TestParseCapsSkippedEntries(t *testing.T) {
	var b strings.Builder
	b.WriteString("trojan://pw@203.0.113.5:443?sni=a.example.com#ok\n")
	long := strings.Repeat("x", 5000)
	for i := range 20000 {
		fmt.Fprintf(&b, "%s%d://junk\n", long[:300], i%3)
	}
	res, err := Parse([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != MaxSkipped || res.SkippedTotal != 20000 || res.SkippedMore() != 20000-MaxSkipped {
		t.Fatalf("kept %d, total %d, more %d", len(res.Skipped), res.SkippedTotal, res.SkippedMore())
	}
	for _, s := range res.Skipped {
		if len([]rune(s.Kind)) > 64 || len([]rune(s.Reason)) > 200 {
			t.Fatalf("entry not truncated: %d, %d", len(s.Kind), len(s.Reason))
		}
	}
}
