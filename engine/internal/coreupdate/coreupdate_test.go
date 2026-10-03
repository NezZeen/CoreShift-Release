package coreupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"coreshift/engine/internal/core"
)

func TestAssetNames(t *testing.T) {
	for _, c := range []struct {
		k            core.Kind
		goos, goarch string
		want         string
	}{
		{core.Xray, "windows", "amd64", "Xray-windows-64.zip"},
		{core.Xray, "linux", "arm64", "Xray-linux-arm64-v8a.zip"},
		{core.SingBox, "windows", "amd64", "sing-box-1.14.2-windows-amd64.zip"},
		{core.SingBox, "linux", "amd64", "sing-box-1.14.2-linux-amd64.tar.gz"},
		{core.Mihomo, "windows", "amd64", "mihomo-windows-amd64-v1-v1.14.2.zip"},
		{core.Mihomo, "linux", "arm64", "mihomo-linux-arm64-v1.14.2.gz"},
	} {
		got, err := assetName(c.k, "v1.14.2", c.goos, c.goarch)
		if err != nil || got != c.want {
			t.Errorf("%s %s/%s = %q, %v; want %q", c.k, c.goos, c.goarch, got, err, c.want)
		}
	}
	if _, err := assetName(core.Xray, "v1", "darwin", "arm64"); err == nil {
		t.Error("darwin has no build here")
	}
}

func buildFake(t *testing.T, dir, name, version string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", out, "./testdata/fakever")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake core: %v\n%s", err, b)
	}
	return out
}

type fakeGitHub struct {
	*httptest.Server
	archive []byte
	digest  string
}

func newFakeGitHub(t *testing.T, asset string, files map[string][]byte) *fakeGitHub {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, b := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write(b)
	}
	zw.Close()
	sum := sha256.Sum256(buf.Bytes())
	g := &fakeGitHub{archive: buf.Bytes(), digest: "sha256:" + hex.EncodeToString(sum[:])}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9", "assets": []any{
				map[string]any{"name": "other.zip", "browser_download_url": g.URL + "/other", "size": 1, "digest": "sha256:00"},
				map[string]any{"name": asset, "browser_download_url": g.URL + "/" + repoOf(r.URL.Path) + "/releases/download/v9.9.9/" + asset,
					"size": len(g.archive), "digest": g.digest},
			}})
		case strings.Contains(r.URL.Path, "/releases/download/"):
			_, _ = w.Write(g.archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(g.Close)
	old, oldDL := APIBase, DownloadBase
	APIBase, DownloadBase = g.URL, g.URL
	t.Cleanup(func() { APIBase, DownloadBase = old, oldDL })
	return g
}

// repoOf is OWNER/REPO of an API path /repos/OWNER/REPO/releases/latest.
func repoOf(path string) string {
	p := strings.Split(strings.TrimPrefix(path, "/repos/"), "/")
	return p[0] + "/" + p[1]
}

func TestInstallReplacesCoreAndLibraries(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("no release builds for this platform")
	}
	src := t.TempDir()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	newBin, _ := os.ReadFile(buildFake(t, src, "sing-box"+exe, "9.9.9"))

	cores := filepath.Join(t.TempDir(), "sing-box-1.0.0")
	os.MkdirAll(cores, 0o755)
	bin := buildFake(t, cores, "sing-box"+exe, "1.0.0")
	lib := filepath.Join(cores, "libcronet.dll")
	os.WriteFile(lib, []byte("old library"), 0o644)

	asset, _ := assetName(core.SingBox, "v9.9.9", runtime.GOOS, runtime.GOARCH)
	if !strings.HasSuffix(asset, ".zip") {
		t.Skip("the fake release is a zip")
	}
	newFakeGitHub(t, asset, map[string][]byte{
		"sing-box-9.9.9/sing-box" + exe: newBin,
		"sing-box-9.9.9/libcronet.dll":  []byte("new library"),
		"sing-box-9.9.9/unrelated.dll":  []byte("not installed: nothing like it next to the core"),
		"sing-box-9.9.9/LICENSE":        []byte("license"),
	})
	c := http.DefaultClient
	rel, err := Latest(context.Background(), c, core.SingBox)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "9.9.9" || rel.Asset != asset {
		t.Fatalf("release = %+v", rel)
	}
	v, err := Install(context.Background(), c, rel, bin)
	if err != nil || v != "9.9.9" {
		t.Fatalf("Install = %q, %v", v, err)
	}
	if b, _ := os.ReadFile(lib); string(b) != "new library" {
		t.Errorf("library = %q", b)
	}
	if _, err := os.Stat(filepath.Join(cores, "unrelated.dll")); err == nil {
		t.Error("installed a library the core did not have")
	}
	if got, _ := core.Version(context.Background(), core.SingBox, bin); got != "9.9.9" {
		t.Errorf("installed version = %q", got)
	}
	Cleanup(map[core.Kind]string{core.SingBox: bin})
	left, _ := os.ReadDir(cores)
	if len(left) != 2 {
		t.Errorf("left after cleanup: %v", left)
	}
}

func TestInstallRejectsBadDownloads(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the fake release is the Windows zip")
	}
	cores := t.TempDir()
	bin := buildFake(t, cores, "sing-box.exe", "1.0.0")
	asset, _ := assetName(core.SingBox, "v9.9.9", runtime.GOOS, runtime.GOARCH)
	g := newFakeGitHub(t, asset, map[string][]byte{"sing-box.exe": []byte("not a program")})
	rel, err := Latest(context.Background(), http.DefaultClient, core.SingBox)
	if err != nil {
		t.Fatal(err)
	}

	// A new file that does not run puts the old one back.
	if _, err := Install(context.Background(), http.DefaultClient, rel, bin); err == nil || !strings.Contains(err.Error(), "kept the old one") {
		t.Fatalf("broken core installed: %v", err)
	}
	if v, err := core.Version(context.Background(), core.SingBox, bin); v != "1.0.0" {
		t.Errorf("after rollback: %q, %v", v, err)
	}

	// A wrong checksum stops before anything is touched.
	rel.SHA256 = strings.Repeat("0", 64)
	if _, err := Install(context.Background(), http.DefaultClient, rel, bin); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if v, _ := core.Version(context.Background(), core.SingBox, bin); v != "1.0.0" {
		t.Errorf("after bad checksum: %q", v)
	}
	Cleanup(map[core.Kind]string{core.SingBox: bin})
	if left, _ := os.ReadDir(cores); len(left) != 1 {
		t.Errorf("left: %v", left)
	}
	_ = g
}
