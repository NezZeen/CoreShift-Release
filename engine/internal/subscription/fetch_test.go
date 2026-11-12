package subscription

import (
	"context"
	"encoding/base64"
	"fmt"
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
	d := ThisDevice()
	if want := d.ForPanel(srv.URL+"/sub/token", false).HWID; hwid != want || want == "" {
		t.Errorf("x-hwid = %q, want %q", hwid, want)
	}
	// A subscription added before per-panel ids keeps the old one, so the
	// panel does not count this machine as a new device.
	if _, err := FetchAs(context.Background(), srv.Client(), srv.URL+"/sub/token", "", true); err == nil {
		t.Fatal("placeholders accepted")
	}
	if hwid != d.HWID {
		t.Errorf("legacy x-hwid = %q, want %q", hwid, d.HWID)
	}
}

func TestPanelHWID(t *testing.T) {
	d := Device{HWID: "legacy", machine: "machine-guid"}
	a := d.ForPanel("https://panel-a.example/sub/one", false).HWID
	a2 := d.ForPanel("https://PANEL-A.example/api/sub/two?x=1", false).HWID
	b := d.ForPanel("https://panel-b.example/sub/one", false).HWID
	if a != a2 {
		t.Errorf("the same panel gets different ids: %s, %s", a, a2)
	}
	if a == b || a == "legacy" || len(a) != 32 {
		t.Errorf("panel ids %q and %q", a, b)
	}
	if got := d.ForPanel("https://panel-a.example/sub/one", true).HWID; got != "legacy" {
		t.Errorf("legacy id = %q", got)
	}
	if got := (Device{}).ForPanel("https://panel-a.example/", false).HWID; got != "" {
		t.Errorf("unknown machine sends %q", got)
	}
	if got := PanelHost("https://User:pw@Panel.Example:8443/sub/x"); got != "panel.example:8443" {
		t.Errorf("PanelHost = %q", got)
	}
}

func TestRedirectPolicyStripsDeviceHeadersAcrossHosts(t *testing.T) {
	var gotOther http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOther = r.Header.Clone()
		fmt.Fprint(w, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#n")
	}))
	defer other.Close()
	var gotOrigin http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/same" {
			gotOrigin = r.Header.Clone()
			fmt.Fprint(w, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#n")
			return
		}
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer origin.Close()
	// 127.0.0.1 with different ports are different hosts for the policy.
	if _, err := Fetch(context.Background(), http.DefaultClient, origin.URL+"/start", ""); err != nil {
		t.Fatal(err)
	}
	for _, h := range deviceHeaders {
		if gotOther.Get(h) != "" {
			t.Errorf("%s leaked to another host", h)
		}
	}
	if gotOther.Get("User-Agent") == "" {
		t.Error("ordinary headers must survive the redirect")
	}
	_ = gotOrigin

	// The machine may have no device id, so the end-to-end run above can be
	// vacuous: check the policy directly too.
	first, _ := http.NewRequest("GET", "https://a.example/sub", nil)
	next, _ := http.NewRequest("GET", "https://b.example/sub", nil)
	for _, h := range deviceHeaders {
		next.Header.Set(h, "x")
	}
	next.Header.Set("Accept", "*/*")
	if err := RedirectPolicy(next, []*http.Request{first}); err != nil {
		t.Fatal(err)
	}
	for _, h := range deviceHeaders {
		if next.Header.Get(h) != "" {
			t.Errorf("%s kept for another host", h)
		}
	}
	if next.Header.Get("Accept") == "" {
		t.Error("Accept dropped")
	}
	same, _ := http.NewRequest("GET", "https://A.example/other", nil)
	same.Header.Set("x-hwid", "x")
	if err := RedirectPolicy(same, []*http.Request{first}); err != nil || same.Header.Get("x-hwid") == "" {
		t.Errorf("same host must keep device headers, err = %v", err)
	}
}

func TestRedirectPolicyLimitsAndDowngrade(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/again", http.StatusFound)
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), http.DefaultClient, srv.URL, ""); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("loop: err = %v", err)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("downgrade followed") }))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer tls.Close()
	if _, err := Fetch(context.Background(), tls.Client(), tls.URL, ""); err == nil || !strings.Contains(err.Error(), "https to http") {
		t.Fatalf("downgrade: err = %v", err)
	}
}
