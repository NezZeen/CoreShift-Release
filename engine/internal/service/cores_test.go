package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

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
