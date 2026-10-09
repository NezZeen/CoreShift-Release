package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestParseGitLabSource(t *testing.T) {
	s, err := ParseSource(MirrorSource)
	if err != nil || !s.GitLab || !s.Public || s.Repo != "NezZeen/coreshift" || s.String() != MirrorSource || s.Site() != "GitLab" {
		t.Fatalf("mirror = %+v, %v", s, err)
	}
	if s, err := ParseSource("gitlab-public:group/sub/project"); err != nil || s.Repo != "group/sub/project" {
		t.Errorf("subgroup = %+v, %v", s, err)
	}
	for _, goos := range []string{"linux", "windows", "android"} {
		if MirrorSourceFor(goos) != MirrorSource {
			t.Errorf("mirror for %s = %s", goos, MirrorSourceFor(goos))
		}
	}
	if s, _ := ParseSource(PublicSource); s.GitLab || s.Site() != "GitHub" {
		t.Errorf("GitHub taken for GitLab: %+v", s)
	}
	for _, bad := range []string{"gitlab-public:", "gitlab-public:project", "gitlab-public:a/../b", "gitlab-public:a//b",
		"gitlab-public:/a/b", "gitlab-public:a/b/", "gitlab-public:a/b?x", "gitlab-public:a/b c", "gitlab:a/b", "gitlab-public:-a/b"} {
		if _, err := ParseSource(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// gitlabFake plays gitlab.com: the releases API of project o/mirror and its
// generic package registry.
type gitlabFake struct {
	*httptest.Server
	mu       sync.Mutex
	files    map[string][]byte // by package path
	releases string            // the API's answer, with %[1]s for the server
	asked    []string
	leaked   bool
}

func newGitLabFake(t *testing.T) *gitlabFake {
	f := &gitlabFake{files: map[string][]byte{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.asked = append(f.asked, r.URL.RequestURI())
		for _, h := range []string{"Authorization", "Private-Token", "Job-Token", "Cookie"} {
			if r.Header.Get(h) != "" {
				f.leaked = true
			}
		}
		switch {
		case r.URL.RequestURI() == "/api/v4/projects/o%2Fmirror/releases?per_page=30":
			fmt.Fprintf(w, f.releases, f.URL)
		case strings.HasPrefix(r.URL.Path, "/o/mirror/-/releases/"):
			// A release's direct link redirects to the package file.
			i := strings.Index(r.URL.Path, "/downloads/")
			http.Redirect(w, r, f.URL+"/api/v4/projects/7/packages/generic/coreshift/0.3.0/"+r.URL.Path[i+len("/downloads/"):], http.StatusFound)
		default:
			b, ok := f.files[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write(b)
		}
	}))
	t.Cleanup(f.Close)
	old := GitLabBase
	GitLabBase = f.URL
	t.Cleanup(func() { GitLabBase = old })
	return f
}

func (f *gitlabFake) put(name string, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files["/api/v4/projects/7/packages/generic/coreshift/0.3.0/"+name] = b
}

func (f *gitlabFake) set(releases string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases, f.asked = releases, nil
}

// pkg is a link to a file of the package registry.
func pkg(name string) string {
	return fmt.Sprintf(`{"name":%q,"url":"%%[1]s/api/v4/projects/7/packages/generic/coreshift/0.3.0/%s","link_type":"package"}`, name, name)
}

func TestGitLabSource(t *testing.T) {
	k := newKey(t)
	body, sig := release(t, k, func(m *Manifest) { m.Installer = "CoreShift-Setup.exe" })
	f := newGitLabFake(t)
	f.put("latest.json", body)
	f.put("latest.json.sig", sig)
	f.put("CoreShift-Setup.exe", installer)
	// Newest first: an upcoming release, then an Android-only one, then
	// the Windows one, linked once through the package registry and once
	// through the release's direct link.
	f.set(`[
		{"tag_name":"v0.4.0","upcoming_release":true,"assets":{"links":[` + pkg("latest.json") + `]}},
		{"tag_name":"v0.3.1","assets":{"links":[` + pkg("latest-android.json") + `]}},
		{"tag_name":"v0.3.0","_links":{"self":"https://gitlab.com/o/mirror/-/releases/v0.3.0"},"assets":{"links":[` +
		pkg("latest.json") + `,` + pkg("latest.json.sig") + `,
			{"name":"CoreShift-Setup.exe","url":"https://evil.example/x.exe","direct_asset_url":"%[1]s/o/mirror/-/releases/v0.3.0/downloads/CoreShift-Setup.exe"}]}}]`)

	src, _ := ParseSource("gitlab-public:o/mirror")
	rel, err := Check(context.Background(), http.DefaultClient, src, ManifestName, []string{k.pub})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.3.0" || rel.Page != "https://gitlab.com/o/mirror/-/releases/v0.3.0" || rel.Source().String() != "gitlab-public:o/mirror" {
		t.Errorf("release %+v, page %q", rel.Manifest, rel.Page)
	}
	path, err := Download(context.Background(), http.DefaultClient, rel, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(installer) {
		t.Errorf("downloaded %q", b)
	}
	if f.leaked {
		t.Error("credentials were sent to GitLab")
	}

	// A different file under the name is refused while it downloads.
	f.put("CoreShift-Setup.exe", []byte("MZ evil installer"))
	if _, err := Download(context.Background(), http.DefaultClient, rel, t.TempDir()); err == nil {
		t.Error("a tampered installer downloaded")
	}

	// Signed with another key: refused, and not a reason to look elsewhere.
	_, err = Check(context.Background(), http.DefaultClient, src, ManifestName, []string{newKey(t).pub})
	if err == nil || !strings.Contains(err.Error(), "signature") || Unreachable(err) {
		t.Errorf("another key: %v", err)
	}

	// No release has the platform's manifest.
	_, err = Check(context.Background(), http.DefaultClient, src, ManifestFor("linux"), []string{k.pub})
	if err == nil || !strings.Contains(err.Error(), "no release has") || Unreachable(err) {
		t.Errorf("no linux manifest: %v", err)
	}
}

// Addresses on other hosts in the API's answer are never fetched.
func TestGitLabForeignLinks(t *testing.T) {
	k := newKey(t)
	body, sig := release(t, k, func(m *Manifest) { m.Installer = "CoreShift.apk" })
	var foreign bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreign = true
		w.Write(body)
	}))
	defer evil.Close()
	evilURL := strings.Replace(evil.URL, "127.0.0.1", "localhost", 1)
	f := newGitLabFake(t)
	f.put("latest-android.json", body)
	f.put("latest-android.json.sig", sig)
	src, _ := ParseSource("gitlab-public:o/mirror")

	for name, links := range map[string]string{
		"manifest elsewhere": `{"name":"latest-android.json","url":"` + evilURL + `/latest-android.json"},` + pkg("latest-android.json.sig"),
		"same port, other host": `{"name":"latest-android.json","url":"` + strings.Replace(f.URL, "127.0.0.1", "localhost", 1) +
			`/api/v4/projects/7/packages/generic/coreshift/0.3.0/latest-android.json"},` + pkg("latest-android.json.sig"),
		"not a package":       `{"name":"latest-android.json","url":"%[1]s/o/other/-/raw/main/latest-android.json"},` + pkg("latest-android.json.sig"),
		"with credentials":    `{"name":"latest-android.json","url":"` + strings.Replace(f.URL, "http://", "http://u:p@", 1) + `/api/v4/projects/7/packages/generic/coreshift/0.3.0/latest-android.json"},` + pkg("latest-android.json.sig"),
		"installer elsewhere": pkg("latest-android.json") + `,` + pkg("latest-android.json.sig") + `,{"name":"CoreShift.apk","url":"` + evilURL + `/CoreShift.apk"}`,
		"no installer":        pkg("latest-android.json") + `,` + pkg("latest-android.json.sig"),
	} {
		f.set(`[{"tag_name":"v0.3.0","assets":{"links":[` + links + `]}}]`)
		_, err := Check(context.Background(), http.DefaultClient, src, ManifestFor("android"), []string{k.pub})
		if err == nil || Unreachable(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if foreign {
		t.Error("a foreign host was asked")
	}
	if got := gitlabReleasePage("o/mirror", "https://evil.example/o/mirror/-/releases/v1"); got != "https://gitlab.com/o/mirror/-/releases" {
		t.Errorf("foreign page kept: %s", got)
	}
}

func TestGitLabReleasePage(t *testing.T) {
	const path = "o/r"
	for page, want := range map[string]string{
		"https://gitlab.com/o/r/-/releases/v0.7.0":  "https://gitlab.com/o/r/-/releases/v0.7.0",
		"https://gitlab.com/o/x/-/releases/v0.7.0":  "https://gitlab.com/o/r/-/releases",
		"https://github.com/o/r/-/releases/v0.7.0":  "https://gitlab.com/o/r/-/releases",
		"http://gitlab.com/o/r/-/releases/v1":       "https://gitlab.com/o/r/-/releases",
		"https://u@gitlab.com/o/r/-/releases/v1":    "https://gitlab.com/o/r/-/releases",
		"https://gitlab.com/o/r/-/releases/../../x": "https://gitlab.com/o/r/-/releases",
		"https://gitlab.com/o/r/-/releases/v1?x=1":  "https://gitlab.com/o/r/-/releases",
		"https://gitlab.com/o/r/-/releases/":        "https://gitlab.com/o/r/-/releases",
		"":                                          "https://gitlab.com/o/r/-/releases",
	} {
		if got := gitlabReleasePage(path, page); got != want {
			t.Errorf("gitlabReleasePage(%q) = %q, want %q", page, got, want)
		}
	}
}

// What makes the mirror worth asking: the source out of reach, not an
// answer it gave.
func TestUnreachable(t *testing.T) {
	// Servers that answer with code, or with a page that is not JSON (0).
	serve := func(code int) string {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if code == 0 {
				w.Write([]byte("<html>"))
				return
			}
			w.WriteHeader(code)
		}))
		t.Cleanup(s.Close)
		return s.URL
	}
	oldBase, oldGitLab := APIBase, GitLabBase
	defer func() { APIBase, GitLabBase = oldBase, oldGitLab }()
	gl, _ := ParseSource("gitlab-public:o/mirror")
	gh, _ := ParseSource(PublicSource)
	for code, want := range map[int]bool{429: true, 403: true, 502: true, 503: true, 404: false, 401: false, 0: false} {
		GitLabBase, APIBase = serve(code), serve(code)
		for _, src := range []Source{gl, gh} {
			_, err := Check(context.Background(), http.DefaultClient, src, ManifestName, nil)
			if err == nil || Unreachable(err) != want {
				t.Errorf("%s %d: %v, unreachable %v", src.Site(), code, err, Unreachable(err))
			}
		}
	}

	// No connection at all.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	APIBase = "http://" + l.Addr().String()
	l.Close()
	if _, err := Check(context.Background(), http.DefaultClient, gh, ManifestName, nil); !Unreachable(err) {
		t.Errorf("refused: %v", err)
	}

	// Through the proxy and directly: unreachable only if both were.
	netErr := MarkUnreachable(errors.New("dial tcp: i/o timeout"))
	sig := errors.New("update signature does not match the release key")
	if !Unreachable(fmt.Errorf("through the proxy: %w; directly: %w", netErr, netErr)) {
		t.Error("both ways out of reach")
	}
	if Unreachable(fmt.Errorf("through the proxy: %w; directly: %w", sig, netErr)) {
		t.Error("an answer through the proxy taken for no answer")
	}
	if Unreachable(nil) || Unreachable(sig) || MarkUnreachable(nil) != nil {
		t.Error("nil or an answer taken for out of reach")
	}
}
