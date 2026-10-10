package subscription

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func headers(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func b64(s string) string { return "base64:" + base64.StdEncoding.EncodeToString([]byte(s)) }

func TestAnnounceHeaders(t *testing.T) {
	long := strings.Repeat("я", 1500)
	tests := []struct {
		name     string
		h        http.Header
		text     string
		link     string
		wantLong bool
	}{
		{"absent", headers(), "", "", false},
		{"plain", headers("Announce", "  Технические работы в пятницу  "), "Технические работы в пятницу", "", false},
		{"base64", headers("Announce", b64("Скидка 20% до воскресенья")), "Скидка 20% до воскресенья", "", false},
		{"base64 unpadded url-safe", headers("Announce", "base64:"+base64.RawURLEncoding.EncodeToString([]byte("Привет ✓"))), "Привет ✓", "", false},
		{"base64 prefix in capitals", headers("Announce", "BASE64:"+base64.StdEncoding.EncodeToString([]byte("hello"))), "hello", "", false},
		{"base64 broken", headers("Announce", "base64:@@@not base64@@@"), "", "", false},
		{"base64 empty", headers("Announce", "base64:"), "", "", false},
		{"with link", headers("Announce", "Новости", "Announce-Url", "https://example.com/news?id=1"), "Новости", "https://example.com/news?id=1", false},
		{"http link", headers("Announce", "x", "Announce-Url", "http://example.com/n"), "x", "http://example.com/n", false},
		{"link without text", headers("Announce-Url", "https://example.com/news"), "", "", false},
		{"javascript link", headers("Announce", "x", "Announce-Url", "javascript:alert(1)"), "x", "", false},
		{"file link", headers("Announce", "x", "Announce-Url", "file:///C:/Windows/System32/calc.exe"), "x", "", false},
		{"telegram link", headers("Announce", "x", "Announce-Url", "tg://resolve?domain=a"), "x", "", false},
		{"link without host", headers("Announce", "x", "Announce-Url", "https:///path"), "x", "", false},
		{"link with credentials", headers("Announce", "x", "Announce-Url", "https://user:pw@example.com/"), "x", "", false},
		{"link with a space", headers("Announce", "x", "Announce-Url", "https://example.com/a b"), "x", "", false},
		{"relative link", headers("Announce", "x", "Announce-Url", "/news"), "x", "", false},
		{"control characters", headers("Announce", "a\x00b\x07c\x1b[31md\u202ee\u200bf"), "abc[31mdef", "", false},
		{"line breaks kept", headers("Announce", b64("one\r\ntwo\r\n\r\n\r\n\r\nthree\tfour\x00")), "one\ntwo\n\nthree four", "", false},
		{"only control characters", headers("Announce", "\x01\x02 \t"), "", "", false},
		{"too long", headers("Announce", long), "", "", true},
		{"too long in base64", headers("Announce", b64(long)), "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, link := parseAnnounce(tt.h)
			if tt.wantLong {
				if n := len([]rune(text)); n != maxAnnounce || !strings.HasSuffix(text, "…") {
					t.Errorf("text has %d characters, suffix %q", n, text[len(text)-3:])
				}
				return
			}
			if text != tt.text || link != tt.link {
				t.Errorf("got (%q, %q), want (%q, %q)", text, link, tt.text, tt.link)
			}
		})
	}
}

func TestSupportLinksAreFiltered(t *testing.T) {
	info := parseInfo(headers(
		"Support-Url", "https://t.me/example_support",
		"Profile-Web-Page-Url", "javascript:alert(1)",
	))
	if info.SupportURL != "https://t.me/example_support" || info.WebPageURL != "" {
		t.Errorf("support=%q page=%q", info.SupportURL, info.WebPageURL)
	}
	if got := parseInfo(headers("Support-Url", "mailto:help@example.com")).SupportURL; got != "mailto:help@example.com" {
		t.Errorf("mail support = %q", got)
	}
	if got := parseInfo(headers("Support-Url", "file:///etc/passwd")).SupportURL; got != "" {
		t.Errorf("file support = %q", got)
	}
}

// A refresh whose response has no Announce header carries no announcement:
// what the store keeps is replaced by it (store.infoOf).
func TestFetchAnnounceFollowsHeader(t *testing.T) {
	announce := b64("Профилактика ночью")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if announce != "" {
			w.Header().Set("Announce", announce)
			w.Header().Set("Announce-Url", "https://example.com/status")
		}
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(linkList))))
	}))
	defer srv.Close()
	get := func() Info {
		f, err := Fetch(context.Background(), srv.Client(), srv.URL+"/sub/token", "")
		if err != nil {
			t.Fatal(err)
		}
		return f.Info
	}
	if i := get(); i.Announce != "Профилактика ночью" || i.AnnounceURL != "https://example.com/status" {
		t.Errorf("announcement = %q %q", i.Announce, i.AnnounceURL)
	}
	announce = ""
	if i := get(); i.Announce != "" || i.AnnounceURL != "" {
		t.Errorf("announcement not cleared: %q %q", i.Announce, i.AnnounceURL)
	}
}
