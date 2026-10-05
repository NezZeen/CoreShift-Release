package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/coreupdate"
)

// The fake cores report 1.2.3; a "latest" release that is older, or the
// same, is not installed, and not even downloaded.
func TestUpdateCoreNeverGoesBack(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("no core releases for this platform")
	}
	var downloads atomic.Int32
	var tag atomic.Value
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/download/") {
			downloads.Add(1)
			http.NotFound(w, r)
			return
		}
		tg := tag.Load().(string)
		asset := "Xray-" + runtime.GOOS + "-64.zip"
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tg, "assets": []any{map[string]any{
			"name": asset, "browser_download_url": srv.URL + "/XTLS/Xray-core/releases/download/" + tg + "/" + asset,
			"size": 10, "digest": "sha256:" + strings.Repeat("b", 64),
		}}})
	}))
	defer srv.Close()
	oldAPI, oldDL := coreupdate.APIBase, coreupdate.DownloadBase
	coreupdate.APIBase, coreupdate.DownloadBase = srv.URL, srv.URL
	defer func() { coreupdate.APIBase, coreupdate.DownloadBase = oldAPI, oldDL }()
	if runtime.GOARCH != "amd64" {
		t.Skip("the fake release is for amd64")
	}

	h := newHarness(t, nil)
	if v := h.svc.CoreVersions(context.Background())[core.Xray]; v != "1.2.3" {
		t.Fatalf("fake core version %q", v)
	}
	for _, tg := range []string{"v1.2.0", "v1.2.3", "v0.9.9"} {
		tag.Store(tg)
		if _, err := h.svc.UpdateCore(context.Background(), core.Xray); err == nil || !strings.Contains(err.Error(), "not newer") {
			t.Errorf("%s over 1.2.3: %v", tg, err)
		}
	}
	if n := downloads.Load(); n != 0 {
		t.Errorf("%d downloads of an older release", n)
	}
	// A newer one is fetched (and here fails to download).
	tag.Store("v1.3.0")
	if _, err := h.svc.UpdateCore(context.Background(), core.Xray); err == nil || downloads.Load() == 0 {
		t.Errorf("newer release: %v, %d downloads", err, downloads.Load())
	}
}

// The start and the app opening ask for the versions at the same time:
// each core is started once for both, and once only.
func TestCoreVersionsAskEachCoreOnce(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, func(c *Config) {
		c.coreVersion = func(_ context.Context, k core.Kind, _ string) (string, error) {
			calls.Add(1)
			time.Sleep(100 * time.Millisecond) // starting a core takes a while
			if k == core.Mihomo {
				return "", errors.New("no version printed")
			}
			return "1.2.3", nil
		}
	})
	installed := len(h.svc.cfg.Binaries)
	go h.svc.WarmUp(context.Background())
	time.Sleep(20 * time.Millisecond)
	results := make(chan map[core.Kind]string, 4)
	for range 4 {
		go func() { results <- h.svc.CoreVersions(context.Background()) }()
	}
	for range 4 {
		if v := <-results; len(v) != installed-1 || v[core.Xray] != "1.2.3" {
			t.Errorf("versions %v", v)
		}
	}
	if n := calls.Load(); n != int32(installed) {
		t.Errorf("%d core starts for %d cores", n, installed)
	}
	// Known now: asked again, only the core that did not tell is.
	h.svc.CoreVersions(context.Background())
	if n := calls.Load(); n != int32(installed)+1 {
		t.Errorf("%d core starts after asking again, want %d", n, installed+1)
	}
}

// A request made while the connection comes up waits for it, rather than
// going direct before the tunnel exists, and then goes through the core.
func TestRequestsWaitForTheConnection(t *testing.T) {
	h := newHarness(t, nil)
	h.svc.setStatus(Status{State: Connecting})
	var proxied atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- h.svc.viaProxyOrDirect(context.Background(), time.Second, func(c *http.Client) error {
			tr := c.Transport.(*http.Transport)
			if tr.Proxy != nil {
				proxied.Store(true)
			}
			return nil
		})
	}()
	select {
	case <-done:
		t.Fatal("the request did not wait for the connection")
	case <-time.After(300 * time.Millisecond):
	}
	h.svc.setStatus(Status{State: Connected})
	select {
	case err := <-done:
		if err != nil || !proxied.Load() {
			t.Errorf("err %v, through the proxy %v", err, proxied.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the request never ran")
	}
}

// When the proxy and the direct way both fail, both reasons are told: a
// rate limit through the proxy must not read as "no connection".
func TestBothReasonsOfAFailedRequest(t *testing.T) {
	h := newHarness(t, nil)
	h.svc.setStatus(Status{State: Connected})
	err := h.svc.viaProxyOrDirect(context.Background(), time.Second, func(c *http.Client) error {
		if c.Transport.(*http.Transport).Proxy != nil {
			return errors.New("xray: check for updates: GitHub answered 403 Forbidden")
		}
		return errors.New("xray: check for updates: dial tcp: i/o timeout")
	})
	want := "through the proxy: xray: check for updates: GitHub answered 403 Forbidden; directly: xray: check for updates: dial tcp: i/o timeout"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(err.Error(), h.svc.socks.Pass) {
		t.Error("the error carries the proxy's password")
	}
}
