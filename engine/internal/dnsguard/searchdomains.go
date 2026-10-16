package dnsguard

import (
	"slices"
	"strings"
)

// maxSearchDomains bounds the network's domains taken: a network names one
// or two; a long list is no company's.
const maxSearchDomains = 16

// privateSingleLabel are the one-word suffixes that networks use for their
// own names and no public site has. Any other one-word suffix would be a
// public top-level domain ("ru", "com") that a careless router announced:
// every site under it would then be asked of the network's resolver.
var privateSingleLabel = []string{"corp", "home", "internal", "intranet", "lan", "local", "localdomain", "private"}

// CleanSearchDomains makes the network's DNS suffixes and search domains
// fit to match names by: in lower case, without dots around, valid,
// distinct, and none that is a public top-level domain.
func CleanSearchDomains(in []string) []string {
	var out []string
	for _, d := range in {
		d = strings.Trim(strings.ToLower(strings.TrimSpace(d)), ".")
		if d == "" || len(d) > 253 || slices.Contains(out, d) || !validDomain(d) {
			continue
		}
		if !strings.Contains(d, ".") && !slices.Contains(privateSingleLabel, d) {
			continue
		}
		out = append(out, d)
		if len(out) == maxSearchDomains {
			break
		}
	}
	return out
}

// validDomain reports whether d is a host name made of letters, digits and
// hyphens (punycode included), in labels of up to 63 characters.
func validDomain(d string) bool {
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}
