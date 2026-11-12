package node

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on the text a subscription controls. Real names are a few dozen
// characters; anything longer only serves to push the rest off the screen.
const (
	MaxNameRunes   = 100
	MaxServerRunes = 255
)

// CleanText makes text from an untrusted source safe to show: control and
// format characters are dropped (the latter include the bidirectional
// overrides, such as U+202E, that make "evil.example" read as another
// word), so are the line and paragraph separators, invalid UTF-8 goes, the
// ends are trimmed and the result is cut to at most max runes.
func CleanText(s string, max int) string {
	// Cut before scanning: a name can be megabytes long. Anything past the
	// limit in bytes cannot fit in max runes of what is left.
	if len(s) > 4*max+64 {
		s = strings.ToValidUTF8(s[:4*max+64], "")
	}
	s = strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		s = strings.TrimSpace(string([]rune(s)[:max]))
	}
	return s
}
