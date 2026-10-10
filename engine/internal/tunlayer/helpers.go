package tunlayer

import (
	"net/netip"
	"regexp"
)

// appPatterns match an executable name at the end of a full path, in any
// case: Windows paths keep whatever case the installer chose.
func appPatterns(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = `(?i)(^|[\\/])` + regexp.QuoteMeta(n) + `$`
	}
	return out
}

func ruleSetTags(sets []RuleSet) []string {
	tags := make([]string, len(sets))
	for i, rs := range sets {
		tags[i] = rs.Tag
	}
	return tags
}

func prefixStrings(ps []netip.Prefix) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}
