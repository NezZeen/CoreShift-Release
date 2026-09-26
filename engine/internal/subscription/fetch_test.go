package subscription

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetch(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		h := w.Header()
		h.Set("Subscription-Userinfo", "upload=100; download=200; total=1000; expire=1893456000")
		h.Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("Тест")))
		h.Set("Profile-Update-Interval", "12")
		h.Set("Support-Url", "https://t.me/example_support")
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(linkList))))
	}))
	defer srv.Close()

	f, err := Fetch(context.Background(), srv.Client(), srv.URL+"/sub/token", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != DefaultUserAgent {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if len(f.Nodes) != 3 || f.Format != FormatBase64 {
		t.Errorf("nodes=%d format=%s", len(f.Nodes), f.Format)
	}
	want := Info{
		Title: "Тест", Upload: 100, Download: 200, Total: 1000,
		Expire: time.Unix(1893456000, 0), UpdateInterval: 12 * time.Hour,
		SupportURL: "https://t.me/example_support",
	}
	if f.Info != want {
		t.Errorf("info = %+v\nwant   %+v", f.Info, want)
	}
}

func TestFetchTitleFromContentDisposition(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Disposition", `attachment; filename*=UTF-8''%D0%9C%D0%BE%D0%B9%20VPN`)
	if got := parseInfo(h).Title; got != "Мой VPN" {
		t.Errorf("title = %q", got)
	}
}

func TestFetchErrorsHideToken(t *testing.T) {
	const token = "SECRET-TOKEN-123"

	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer forbidden.Close()
	_, err := Fetch(context.Background(), forbidden.Client(), forbidden.URL+"/sub/"+token, "")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("403: err = %v", err)
	}

	down := httptest.NewServer(http.NotFoundHandler())
	url := down.URL + "/sub/" + token
	down.Close()
	_, err = Fetch(context.Background(), http.DefaultClient, url, "")
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error leaks the subscription token: %v", err)
	}
}

func TestFetchRejectsHugeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := []byte(strings.Repeat("A", 1<<20))
		for range maxBodySize>>20 + 1 {
			w.Write(chunk)
		}
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.Client(), srv.URL, ""); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("err = %v, want size limit error", err)
	}
}

func TestFetchSendsDeviceAndRejectsPlaceholders(t *testing.T) {
	var hwid string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hwid = r.Header.Get("x-hwid")
		// What Remnawave sends a client without x-hwid when the device limit is on.
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(
			"vless://00000000-0000-0000-0000-000000000000@0.0.0.0:1?type=tcp#App%20not%20supported\n"))))
	}))
	defer srv.Close()

	f, err := Fetch(context.Background(), srv.Client(), srv.URL+"/sub/token", "")
	if err == nil || !strings.Contains(err.Error(), "App not supported") || len(f.Nodes) != 0 {
		t.Errorf("err = %v, nodes = %d", err, len(f.Nodes))
	}
	if want := ThisDevice().HWID; hwid != want || want == "" {
		t.Errorf("x-hwid = %q, want %q", hwid, want)
	}
}
