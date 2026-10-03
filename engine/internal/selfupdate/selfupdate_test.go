package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testKey struct {
	pub  string
	priv string
}

func newKey(t *testing.T) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv.Seed())}
}

var installer = []byte("MZ fake installer")

// release returns a signed manifest and signature for the fake installer.
func release(t *testing.T, k testKey, edit func(*Manifest)) (body, sig []byte) {
	t.Helper()
	sum := sha256.Sum256(installer)
	m := Manifest{
		Version: "0.3.0", Build: 7, Commit: "abc1234",
		Installer: "coreshift-setup-0.3.0-b7.exe", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(installer)),
		Published: time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC),
	}
	if edit != nil {
		edit(&m)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if sig, err = Sign(body, k.priv); err != nil {
		t.Fatal(err)
	}
	return body, sig
}

func TestVerify(t *testing.T) {
	k, other := newKey(t), newKey(t)
	body, sig := release(t, k, nil)
	if m, err := Verify(body, sig, []string{other.pub, k.pub}); err != nil || m.Build != 7 {
		t.Fatalf("Verify = %+v, %v", m, err)
	}
	if _, err := Verify(body, sig, []string{other.pub}); err == nil {
		t.Error("accepted a signature by another key")
	}
	tampered := []byte(strings.Replace(string(body), `"build":7`, `"build":8`, 1))
	if _, err := Verify(tampered, sig, []string{k.pub}); err == nil {
		t.Error("accepted a changed manifest")
	}
	for name, edit := range map[string]func(*Manifest){
		"path in the name": func(m *Manifest) { m.Installer = `..\evil.exe` },
		"not an exe":       func(m *Manifest) { m.Installer = "setup.msi" },
		"bad version":      func(m *Manifest) { m.Version = "1.2" },
		"bad hash":         func(m *Manifest) { m.SHA256 = "abc" },
		"no size":          func(m *Manifest) { m.Size = 0 },
	} {
		body, sig := release(t, k, edit)
		if _, err := Verify(body, sig, []string{k.pub}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if pub, err := PublicKey(k.priv); err != nil || pub != k.pub {
		t.Errorf("PublicKey = %s, %v", pub, err)
	}
}

func TestNewer(t *testing.T) {
	m := Manifest{Version: "0.10.0", Build: 3}
	for _, c := range []struct {
		version string
		build   int
		want    bool
	}{
		{"0.9.9", 50, true}, {"0.10.0", 2, true}, {"0.10.0", 3, false}, {"0.10.1", 1, false}, {"1.0.0", 1, false},
	} {
		if got := m.Newer(c.version, c.build); got != c.want {
			t.Errorf("0.10.0 b3 newer than %s b%d = %v", c.version, c.build, got)
		}
	}
}

func TestParseSource(t *testing.T) {
	if s, err := ParseSource(DefaultSource); err != nil || s.Repo != "NezZeen/coreshift-releases" {
		t.Errorf("default = %+v, %v", s, err)
	}
	dir := t.TempDir()
	if s, err := ParseSource(dir); err != nil || s.Dir != dir {
		t.Errorf("folder = %+v, %v", s, err)
	}
	for _, bad := range []string{"github:owner", "github:a/b/c", "relative\\dir", "https://example.com/latest.json",
		`\\host\share\releases`, `\\?\C:\releases`, `\\.\pipe\x`, "//host/share/releases"} {
		if _, err := ParseSource(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFolderSource(t *testing.T) {
	k := newKey(t)
	dir := t.TempDir()
	body, sig := release(t, k, nil)
	os.WriteFile(filepath.Join(dir, ManifestName), body, 0o644)
	os.WriteFile(filepath.Join(dir, SignatureName), sig, 0o644)
	os.WriteFile(filepath.Join(dir, "coreshift-setup-0.3.0-b7.exe"), installer, 0o644)

	rel, err := Check(context.Background(), http.DefaultClient, Source{Dir: dir}, ManifestName, []string{k.pub})
	if err != nil {
		t.Fatal(err)
	}
	dl := t.TempDir()
	path, err := Download(context.Background(), http.DefaultClient, rel, dl)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(installer) {
		t.Errorf("downloaded %q", b)
	}

	// A different file under the right name is refused.
	os.WriteFile(filepath.Join(dir, "coreshift-setup-0.3.0-b7.exe"), []byte("MZ evil installer"), 0o644)
	if _, err := Download(context.Background(), http.DefaultClient, rel, t.TempDir()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("tampered installer: %v", err)
	}
}

func TestPlatformManifests(t *testing.T) {
	k := newKey(t)
	dir := t.TempDir()
	apk, apkSig := release(t, k, func(m *Manifest) { m.Installer = "coreshift-0.3.0-b7.apk" })
	os.WriteFile(filepath.Join(dir, AndroidManifestName), apk, 0o644)
	os.WriteFile(filepath.Join(dir, AndroidManifestName+".sig"), apkSig, 0o644)
	rel, err := Check(context.Background(), http.DefaultClient, Source{Dir: dir}, ManifestFor("android"), []string{k.pub})
	if err != nil || rel.Installer != "coreshift-0.3.0-b7.apk" {
		t.Fatalf("android: %v %+v", err, rel)
	}
	// The Windows service must not run an APK, nor the phone get an .exe.
	os.WriteFile(filepath.Join(dir, ManifestName), apk, 0o644)
	os.WriteFile(filepath.Join(dir, SignatureName), apkSig, 0o644)
	if _, err := Check(context.Background(), http.DefaultClient, Source{Dir: dir}, ManifestFor("windows"), []string{k.pub}); err == nil {
		t.Error("windows accepted an APK")
	}
	if ManifestFor("windows") != ManifestName {
		t.Error("Windows manifest name changed: installed copies would stop seeing releases")
	}
	// Linux updates with its package: it must never take the Windows
	// installer, nor the APK.
	if ManifestFor("linux") == ManifestName || installerExt(ManifestFor("linux")) == ".exe" {
		t.Error("Linux reads the Windows manifest")
	}
	if _, err := Check(context.Background(), http.DefaultClient, Source{Dir: dir}, ManifestFor("linux"), []string{k.pub}); err == nil {
		t.Error("linux accepted a release without its manifest")
	}
}

func TestGitHubSource(t *testing.T) {
	k := newKey(t)
	body, sig := release(t, k, nil)

	// Assets redirect to storage on another host, as GitHub's do; the
	// token must not follow them there.
	var leaked bool
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked = true
		}
		switch r.URL.Path {
		case "/1":
			w.Write(body)
		case "/2":
			w.Write(sig)
		case "/3":
			w.Write(installer)
		}
	}))
	defer storage.Close()
	storageURL := strings.Replace(storage.URL, "127.0.0.1", "localhost", 1) // another host

	var api *httptest.Server
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch {
		case r.URL.Path == "/repos/owner/releases/releases":
			// Newest first: an Android-only release and a draft come
			// before the Windows one.
			fmt.Fprintf(w, `[
				{"assets":[{"name":"latest-android.json","url":"%[1]s/assets/9","size":1}]},
				{"draft":true,"assets":[{"name":"latest.json","url":"%[1]s/assets/8","size":1}]},
				{"assets":[
					{"name":"latest.json","url":"%[1]s/assets/1","size":%[2]d},
					{"name":"latest.json.sig","url":"%[1]s/assets/2","size":%[3]d},
					{"name":"coreshift-setup-0.3.0-b7.exe","url":"%[1]s/assets/3","size":%[4]d}]}]`,
				api.URL+"/repos/owner/releases/releases", len(body), len(sig), len(installer))
		case strings.HasPrefix(r.URL.Path, "/repos/owner/releases/releases/assets/"):
			if r.Header.Get("Accept") != "application/octet-stream" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			http.Redirect(w, r, storageURL+"/"+filepath.Base(r.URL.Path), http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	oldBase, oldToken := APIBase, token
	APIBase, token = api.URL, "test-token"
	defer func() { APIBase, token = oldBase, oldToken }()

	src, _ := ParseSource("github:owner/releases")
	rel, err := Check(context.Background(), http.DefaultClient, src, ManifestName, []string{k.pub})
	if err != nil {
		t.Fatal(err)
	}
	path, err := Download(context.Background(), http.DefaultClient, rel, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(installer) {
		t.Errorf("downloaded %q", b)
	}
	if leaked {
		t.Error("the token was sent to the storage host")
	}

	token = "wrong"
	if _, err := Check(context.Background(), http.DefaultClient, src, ManifestName, []string{k.pub}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("wrong token: %v", err)
	}
	token = ""
	if _, err := Check(context.Background(), http.DefaultClient, src, ManifestName, []string{k.pub}); err == nil || !strings.Contains(err.Error(), "no token") {
		t.Errorf("no token: %v", err)
	}
}

func TestPublicSource(t *testing.T) {
	s, err := ParseSource(PublicSource)
	if err != nil || !s.Public || s.Repo != "NezZeen/CoreShift-Release" || s.String() != PublicSource {
		t.Fatalf("public = %+v, %v", s, err)
	}
	if s, _ := ParseSource(DefaultSource); s.Public {
		t.Error("the private repository taken for a public one")
	}
	if DefaultSourceFor("linux") != PublicSource || DefaultSourceFor("windows") != DefaultSource || DefaultSourceFor("android") != DefaultSource {
		t.Error("default sources per platform changed")
	}
	if _, err := ParseSource("github-public:owner"); err == nil {
		t.Error("github-public:owner accepted")
	}
}

func TestReleasePage(t *testing.T) {
	const repo = "o/r"
	for page, want := range map[string]string{
		"https://github.com/o/r/releases/tag/v0.7.0": "https://github.com/o/r/releases/tag/v0.7.0",
		"https://github.com/o/x/releases/tag/v0.7.0": "https://github.com/o/r/releases",
		"https://evil.example/o/r/releases/tag/v1":   "https://github.com/o/r/releases",
		"http://github.com/o/r/releases/tag/v1":      "https://github.com/o/r/releases",
		"https://user@github.com/o/r/releases/tag/1": "https://github.com/o/r/releases",
		"https://github.com/o/r/releases/../../x":    "https://github.com/o/r/releases",
		"https://github.com/o/r/releases/tag/v1?x=1": "https://github.com/o/r/releases",
		"https://github.com/o/r/releases/":           "https://github.com/o/r/releases",
		"":                                           "https://github.com/o/r/releases",
	} {
		if got := releasePage(repo, page); got != want {
			t.Errorf("releasePage(%q) = %q, want %q", page, got, want)
		}
	}
}

// A public source is read without the token, which is never sent, and
// still verified with the keys.
func TestPublicGitHubSource(t *testing.T) {
	k := newKey(t)
	body, sig := release(t, k, func(m *Manifest) { m.Installer = "coreshift_0.3.0_amd64.deb" })
	var api *httptest.Server
	var sent bool
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sent = true
		}
		switch r.URL.Path {
		case "/repos/o/pub/releases":
			fmt.Fprintf(w, `[{"html_url":"https://github.com/o/pub/releases/tag/v0.3.0","assets":[
				{"name":"latest-linux.json","url":"%[1]s/a/1","size":%[2]d},
				{"name":"latest-linux.json.sig","url":"%[1]s/a/2","size":%[3]d},
				{"name":"coreshift_0.3.0_amd64.deb","url":"%[1]s/a/3","size":%[4]d}]}]`,
				api.URL+"/repos/o/pub/releases", len(body), len(sig), len(installer))
		case "/repos/o/pub/releases/a/1":
			w.Write(body)
		case "/repos/o/pub/releases/a/2":
			w.Write(sig)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()
	oldBase, oldToken := APIBase, token
	APIBase, token = api.URL, "secret-token"
	defer func() { APIBase, token = oldBase, oldToken }()

	src, _ := ParseSource("github-public:o/pub")
	rel, err := Check(context.Background(), http.DefaultClient, src, ManifestFor("linux"), []string{k.pub})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.3.0" || rel.Page != "https://github.com/o/pub/releases/tag/v0.3.0" {
		t.Errorf("release %+v, page %q", rel.Manifest, rel.Page)
	}
	if sent {
		t.Error("the token was sent to a public source")
	}
	token = ""
	if _, err := Check(context.Background(), http.DefaultClient, src, ManifestFor("linux"), []string{k.pub}); err != nil {
		t.Errorf("without a token: %v", err)
	}
	if _, err := Check(context.Background(), http.DefaultClient, src, ManifestFor("linux"), []string{newKey(t).pub}); err == nil {
		t.Error("a manifest signed with another key accepted")
	}
}
