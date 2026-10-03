package coreupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"coreshift/engine/internal/core"
)

// latestAnswering serves a releases/latest answer with tag and the asset
// for this platform at url(base).
func latestAnswering(t *testing.T, tag string, url func(base, asset string) string) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset, _ := assetName(core.SingBox, tag, runtime.GOOS, runtime.GOARCH)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": []any{
			map[string]any{"name": asset, "browser_download_url": url(srv.URL, asset), "size": 1, "digest": "sha256:" + strings.Repeat("a", 64)},
		}})
	}))
	t.Cleanup(srv.Close)
	old, oldDL := APIBase, DownloadBase
	APIBase, DownloadBase = srv.URL, srv.URL
	t.Cleanup(func() { APIBase, DownloadBase = old, oldDL })
}

func TestLatestTakesOnlyTheProjectsOwnFile(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("no release for this platform")
	}
	latestAnswering(t, "v9.9.9", func(base, asset string) string {
		return base + "/SagerNet/sing-box/releases/download/v9.9.9/" + asset
	})
	if _, err := Latest(context.Background(), http.DefaultClient, core.SingBox); err != nil {
		t.Errorf("the project's file: %v", err)
	}
	for name, url := range map[string]func(base, asset string) string{
		"another host": func(_, asset string) string {
			return "https://evil.example/SagerNet/sing-box/releases/download/v9.9.9/" + asset
		},
		"another project": func(base, asset string) string { return base + "/someone/sing-box/releases/download/v9.9.9/" + asset },
		"another release": func(base, asset string) string { return base + "/SagerNet/sing-box/releases/download/v1.0.0/" + asset },
	} {
		latestAnswering(t, "v9.9.9", url)
		if _, err := Latest(context.Background(), http.DefaultClient, core.SingBox); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	latestAnswering(t, "nightly", func(base, asset string) string { return base + "/SagerNet/sing-box/releases/download/nightly/" + asset })
	if _, err := Latest(context.Background(), http.DefaultClient, core.SingBox); err == nil || !strings.Contains(err.Error(), "not a version") {
		t.Errorf("a tag that is not a version: %v", err)
	}
}
