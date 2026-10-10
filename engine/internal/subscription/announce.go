package subscription

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maxAnnounce is the longest announcement kept, in characters.
	maxAnnounce = 1000
	// maxLink is the longest link kept, in bytes.
	maxLink = 2048
)

// parseAnnounce reads the provider's announcement from the Announce header
// (Remnawave, Happ, Hiddify): plain text, or "base64:…" with UTF-8 text in
// it, the form panels use for anything beyond ASCII. Announce-Url is the
// link to show with it. A missing or unreadable header gives no
// announcement, so a refresh clears the one that was.
func parseAnnounce(h http.Header) (text, link string) {
	raw := strings.TrimSpace(h.Get("Announce"))
	if enc, ok := cutPrefixFold(raw, "base64:"); ok {
		b, err := decodeBase64(strings.TrimSpace(enc))
		if err != nil {
			return "", ""
		}
		raw = string(b)
	}
	text = cleanText(raw, maxAnnounce)
	if text == "" {
		return "", ""
	}
	return text, cleanLink(h.Get("Announce-Url"), "http", "https")
}

// cleanText trims s, drops control and invisible formatting characters
// (bidirectional overrides among them) except line breaks, and keeps at
// most limit characters.
func cleanText(s string, limit int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(s)
	s = strings.Map(func(r rune) rune {
		if r == '\n' || !unicode.IsControl(r) && !unicode.Is(unicode.Cf, r) {
			return r
		}
		return -1
	}, s)
	// A line's edges and runs of blank lines carry nothing.
	lines := strings.Split(s, "\n")
	out := lines[:0]
	blank := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			if blank++; blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	s = strings.TrimSpace(strings.Join(out, "\n"))
	if utf8.RuneCountInString(s) > limit {
		s = strings.TrimSpace(string([]rune(s)[:limit-1])) + "…"
	}
	return s
}

// cleanLink returns raw when it is an absolute link with one of the
// schemes and a host, else "". The app opens it in the browser, so an
// unlisted scheme (file:, javascript:, a program's own) must not get there.
func cleanLink(raw string, schemes ...string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxLink || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, s := range schemes {
		if !strings.EqualFold(u.Scheme, s) {
			continue
		}
		if s == "mailto" && u.Opaque != "" || u.Host != "" && u.User == nil {
			return raw
		}
	}
	return ""
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}
