package subscription

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A Clash subscription made of nested aliases expands to billions of
// values; the YAML decoder must refuse it rather than exhaust memory.
func TestClashAliasBombIsRefused(t *testing.T) {
	var b strings.Builder
	b.WriteString("a0: &a0\n  k: [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i <= 9; i++ {
		refs := strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*a%d, ", i-1), 10), ", ")
		fmt.Fprintf(&b, "a%d: &a%d\n  k: [%s]\n", i, i, refs)
	}
	// Maps all the way down, which the proxies' type takes.
	b.WriteString("proxies:\n  - *a9\n")
	start := time.Now()
	_, err := Parse([]byte(b.String()))
	if err == nil {
		t.Fatal("an alias bomb parsed")
	}
	if !strings.Contains(err.Error(), "alias") {
		t.Fatalf("refused for another reason: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("refusing it took %s", d)
	}
}
