package tunlayer

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// The TUN layer's policy routing on Linux (sing-box's auto_route): its
// routes go into table RouteTable, and its ip rules take the priorities
// from RuleIndex to RuleIndex+ruleSpan. Both differ from sing-box's
// defaults (2022, 9000), so another sing-box on the machine keeps its own.
const (
	RouteTable = 17900
	RuleIndex  = 17900
	// ruleSpan covers every priority sing-box uses after RuleIndex (it
	// goes up to RuleIndex+10), with room to spare.
	ruleSpan = 20
)

// staleRulePrefs returns the priorities of the TUN layer's rules in the
// output of "ip rule show", once per rule: a sing-box killed with SIGKILL
// leaves them behind, as the kernel drops its routes with the interface
// but not the rules.
func staleRulePrefs(out []byte) []int {
	var prefs []int
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		head, _, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		p, err := strconv.Atoi(strings.TrimSpace(head))
		if err == nil && p >= RuleIndex && p <= RuleIndex+ruleSpan {
			prefs = append(prefs, p)
		}
	}
	return prefs
}
