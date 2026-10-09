package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// APIBase is GitHub's API; tests point it at a local server.
var APIBase = "https://api.github.com"

// token reads the releases of a private repository, a "github:" source.
// Builds before 0.8.1 set it in a generated, ignored file (token_gen.go);
// releases now come from the public repository and builds carry none, so
// only "github-public:" and folder sources work without it.
var token string

// HasToken reports whether this build can read the private releases.
func HasToken() bool { return token != "" }

// Source is where releases come from: a GitHub repository, a GitLab
// project (the mirror, see MirrorSource) or a folder.
type Source struct {
	Repo string // OWNER/REPO; for GitLab the project's path, NAMESPACE/PROJECT
	// Public: the repository is public and read without the token, which
	// is then never sent (Linux builds have none, see PublicSource).
	Public bool
	// GitLab: Repo is a public project on gitlab.com (GitLabBase), read
	// without any token.
	GitLab bool
	Dir    string
}

var (
	repoRE = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	// A GitLab project's path: a namespace, maybe with subgroups, and the
	// project.
	gitlabPathRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*){1,4}$`)
)

// ParseSource accepts "github:OWNER/REPO" (private, read with the token),
// "github-public:OWNER/REPO" (public, read without it),
// "gitlab-public:NAMESPACE/PROJECT" (a public project on gitlab.com) or an
// absolute folder path on this computer (not a network path). That the
// folder is reached without links and on a local disk is checked each time
// it is read (checkLocal): the folder may not exist yet when the setting is
// saved.
func ParseSource(s string) (Source, error) {
	s = strings.TrimSpace(s)
	if path, ok := strings.CutPrefix(s, "gitlab-public:"); ok {
		if !gitlabPathRE.MatchString(path) || strings.Contains(path, "..") {
			return Source{}, fmt.Errorf("%q is not gitlab-public:NAMESPACE/PROJECT", s)
		}
		return Source{Repo: path, Public: true, GitLab: true}, nil
	}
	for _, p := range []struct {
		prefix string
		public bool
	}{{"github:", false}, {"github-public:", true}} {
		if repo, ok := strings.CutPrefix(s, p.prefix); ok {
			if !repoRE.MatchString(repo) {
				return Source{}, fmt.Errorf("%q is not %sOWNER/REPO", s, p.prefix)
			}
			return Source{Repo: repo, Public: p.public}, nil
		}
	}
	// A network path (\\host\share) would make the service, which runs as
	// SYSTEM, sign in to that host: the setting is writable by any local user.
	if strings.HasPrefix(s, `\\`) || strings.HasPrefix(s, "//") {
		return Source{}, fmt.Errorf("%q is a network path; use a folder on this computer", s)
	}
	if filepath.IsAbs(s) {
		return Source{Dir: filepath.Clean(s)}, nil
	}
	return Source{}, fmt.Errorf("%q is neither github:OWNER/REPO, gitlab-public:NAMESPACE/PROJECT nor a full folder path", s)
}

// readLocal reads a file of a folder source, which must be on this
// computer (checkLocal).
func readLocal(path string) ([]byte, error) {
	if err := checkLocal(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s Source) String() string {
	if s.GitLab {
		return "gitlab-public:" + s.Repo
	}
	if s.Repo != "" && s.Public {
		return "github-public:" + s.Repo
	}
	if s.Repo != "" {
		return "github:" + s.Repo
	}
	return s.Dir
}

// Site names where s is, for the journal: "GitHub", "GitLab" or "папка".
func (s Source) Site() string {
	switch {
	case s.GitLab:
		return "GitLab"
	case s.Repo != "":
		return "GitHub"
	}
	return "папка"
}

// Release is a verified manifest and where its installer is.
type Release struct {
	Manifest
	// Page is the release's page on GitHub or GitLab, for downloading it by
	// hand (Linux); empty for a folder. It is not signed, so it is only
	// ever an address under the source repository's releases.
	Page      string
	src       Source
	installer string // the asset's API URL (GitHub), its link (GitLab), or the file's path
}

// Source is where the release was found.
func (r Release) Source() Source { return r.src }

// Check reads and verifies the manifest named manifest (see ManifestFor)
// of the latest release.
func Check(ctx context.Context, client *http.Client, src Source, manifest string, keys []string) (Release, error) {
	var rel Release
	var err error
	switch {
	case src.Dir != "":
		rel, err = checkDir(src, manifest, keys)
	case src.GitLab:
		rel, err = checkGitLab(ctx, client, src, manifest, keys)
	default:
		rel, err = checkGitHub(ctx, client, src, manifest, keys)
	}
	if err == nil && filepath.Ext(rel.Installer) != installerExt(manifest) {
		return Release{}, fmt.Errorf("update manifest: %s names %s, not a %s file", manifest, rel.Installer, installerExt(manifest))
	}
	return rel, err
}

func checkDir(src Source, manifest string, keys []string) (Release, error) {
	body, err := readLocal(filepath.Join(src.Dir, manifest))
	if err != nil {
		return Release{}, fmt.Errorf("check for updates: %w", err)
	}
	sig, err := readLocal(filepath.Join(src.Dir, manifest+".sig"))
	if err != nil {
		return Release{}, fmt.Errorf("check for updates: %w", err)
	}
	m, err := Verify(body, sig, keys)
	if err != nil {
		return Release{}, err
	}
	return Release{Manifest: m, src: src, installer: filepath.Join(src.Dir, m.Installer)}, nil
}

func checkGitHub(ctx context.Context, client *http.Client, src Source, manifest string, keys []string) (Release, error) {
	assets, page, err := releaseAssets(ctx, client, src, manifest)
	if err != nil {
		return Release{}, err
	}
	files := map[string][]byte{}
	for _, name := range []string{manifest, manifest + ".sig"} {
		a, ok := assets[name]
		if !ok {
			return Release{}, fmt.Errorf("check for updates: the latest release has no %s", name)
		}
		if files[name], err = fetchAsset(ctx, client, src, a.URL, 1<<20); err != nil {
			return Release{}, err
		}
	}
	m, err := Verify(files[manifest], files[manifest+".sig"], keys)
	if err != nil {
		return Release{}, err
	}
	a, ok := assets[m.Installer]
	if !ok {
		return Release{}, fmt.Errorf("check for updates: the latest release has no %s", m.Installer)
	}
	if a.Size != m.Size {
		return Release{}, fmt.Errorf("check for updates: %s is %d bytes, the manifest says %d", m.Installer, a.Size, m.Size)
	}
	return Release{Manifest: m, Page: page, src: src, installer: a.URL}, nil
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// releaseAssets returns the assets of the newest published release that has
// the file manifest, and its page: a release for one platform does not hide
// the previous one of the other.
func releaseAssets(ctx context.Context, client *http.Client, src Source, manifest string) (map[string]asset, string, error) {
	if token == "" && !src.Public {
		return nil, "", errors.New("this build has no token for the private releases")
	}
	// Newest first.
	req, err := githubRequest(ctx, src, APIBase+"/repos/"+src.Repo+"/releases?per_page=30", "application/vnd.github+json")
	if err != nil {
		return nil, "", err
	}
	resp, err := do(client, req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if err := statusError(resp); err != nil {
		return nil, "", err
	}
	var releases []struct {
		Draft      bool    `json:"draft"`
		Prerelease bool    `json:"prerelease"`
		Page       string  `json:"html_url"`
		Assets     []asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&releases); err != nil {
		return nil, "", decodeError(err)
	}
	for _, rel := range releases {
		if rel.Draft || rel.Prerelease {
			continue
		}
		out := map[string]asset{}
		for _, a := range rel.Assets {
			out[a.Name] = a
		}
		if _, ok := out[manifest]; ok {
			return out, releasePage(src.Repo, rel.Page), nil
		}
	}
	return nil, "", fmt.Errorf("check for updates: no release has %s", manifest)
}

// releasePage keeps page only when it is a release page of repo on GitHub,
// else gives the repository's releases: the address comes unsigned from
// the API, and the user opens it.
func releasePage(repo, page string) string {
	prefix := "/" + repo + "/releases/"
	u, err := url.Parse(page)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		!strings.HasPrefix(u.Path, prefix) || len(u.Path) == len(prefix) || strings.Contains(u.Path, "..") {
		return "https://github.com/" + repo + "/releases"
	}
	return "https://github.com" + u.EscapedPath()
}

// githubRequest adds the token, except for a public source. The asset
// download redirects to another host, and net/http does not forward
// Authorization there.
func githubRequest(ctx context.Context, src Source, rawURL, accept string) (*http.Request, error) {
	// The token goes to GitHub's API only; asset URLs come from its answers.
	if !strings.HasPrefix(rawURL, APIBase+"/") {
		return nil, errors.New("check for updates: unexpected address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if !src.Public {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "CoreShift")
	return req, nil
}

// do runs req, keeping addresses out of the error: asset downloads redirect
// to signed URLs, which are credentials for a while. Any failure here is
// the source out of reach: no address for it, no connection, TLS cut off,
// no answer in time.
func do(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, &unreachableError{fmt.Errorf("check for updates: %w", err)}
	}
	return resp, nil
}

// unreachableError is a source that could not be reached or would not
// answer now: the network failed (see do), the connection broke mid-way,
// or the server refused for the moment (403 and 429 rate limits, 5xx).
// Another source may answer instead (Unreachable). A signature that does
// not match, a release without the file, a 404 are answers, not this.
type unreachableError struct{ err error }

func (e *unreachableError) Error() string { return e.err.Error() }
func (e *unreachableError) Unwrap() error { return e.err }

// MarkUnreachable marks err as a source out of reach, for Unreachable: for
// checks made outside this package, and tests.
func MarkUnreachable(err error) error {
	if err == nil {
		return nil
	}
	return &unreachableError{err}
}

// Unreachable reports whether err is only a source out of reach, so that
// another source, the mirror, may answer instead. Errors joined together
// (the attempts through the proxy and directly) must all be: a real answer
// on one way is the answer.
func Unreachable(err error) bool {
	for err != nil {
		if m, ok := err.(interface{ Unwrap() []error }); ok {
			errs := m.Unwrap()
			for _, e := range errs {
				if !Unreachable(e) {
					return false
				}
			}
			return len(errs) > 0
		}
		if _, ok := err.(*unreachableError); ok {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// decodeError is the error of a list of releases that did not decode: a
// broken connection is the source out of reach, anything else what it
// answered.
func decodeError(err error) error {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	err = fmt.Errorf("check for updates: %w", err)
	if errors.As(err, &syntax) || errors.As(err, &typ) {
		return err
	}
	return &unreachableError{err}
}

// transient marks the statuses that say "not now" rather than "no": rate
// limits and the server's own failures.
func transient(code int, err error) error {
	if code == http.StatusForbidden || code == http.StatusTooManyRequests || code >= 500 {
		return &unreachableError{err}
	}
	return err
}

func statusError(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return transient(resp.StatusCode, githubStatusError(resp))
}

func githubStatusError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errors.New("check for updates: the releases token is invalid or expired (401)")
	case http.StatusNotFound:
		// A private repository the token cannot see looks missing.
		return errors.New("check for updates: no release found, or the token has no access (404)")
	case http.StatusTooManyRequests:
		return errors.New("check for updates: GitHub rate limit, try again later")
	case http.StatusForbidden:
		// GitHub answers 403 when an address has used up its requests
		// without a token (60 an hour).
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return errors.New("check for updates: GitHub rate limit, try again later")
		}
	}
	return fmt.Errorf("check for updates: server returned %s", resp.Status)
}

func fetchAsset(ctx context.Context, client *http.Client, src Source, apiURL string, limit int64) ([]byte, error) {
	req, err := githubRequest(ctx, src, apiURL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	resp, err := do(client, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := statusError(resp); err != nil {
		return nil, err
	}
	return readLimited(resp.Body, limit)
}

// readLimited reads at most limit bytes of a small file: a manifest, its
// signature.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, &unreachableError{fmt.Errorf("check for updates: %w", err)}
	}
	if int64(len(b)) > limit {
		return nil, errors.New("check for updates: file too large")
	}
	return b, nil
}

// readError marks what failed reading the download, not writing it.
type readError struct{ error }

type readMarker struct{ r io.Reader }

func (m readMarker) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if err != nil && err != io.EOF {
		err = readError{err}
	}
	return n, err
}

// Download saves the release's installer into dir, which only
// administrators may write to, and checks it against the manifest. It
// returns the installer's path; a verified earlier download is reused.
func Download(ctx context.Context, client *http.Client, rel Release, dir string) (string, error) {
	dst := filepath.Join(dir, rel.Installer)
	if sum, err := fileSHA256(dst); err == nil && sum == rel.SHA256 {
		return dst, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, rel.Installer+".part*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	err = copyInstaller(ctx, client, rel, io.MultiWriter(tmp, h))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if sum := hex.EncodeToString(h.Sum(nil)); sum != rel.SHA256 {
		return "", fmt.Errorf("download update: checksum mismatch (%s)", rel.Installer)
	}
	os.Remove(dst)
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", err
	}
	return dst, nil
}

func copyInstaller(ctx context.Context, client *http.Client, rel Release, w io.Writer) error {
	var r io.ReadCloser
	if rel.src.Dir != "" {
		if err := checkLocal(rel.installer); err != nil {
			return fmt.Errorf("download update: %w", err)
		}
		f, err := os.Open(rel.installer)
		if err != nil {
			return fmt.Errorf("download update: %w", err)
		}
		r = f
	} else if rel.src.GitLab {
		body, err := gitlabOpen(ctx, client, rel.installer)
		if err != nil {
			return err
		}
		r = body
	} else {
		req, err := githubRequest(ctx, rel.src, rel.installer, "application/octet-stream")
		if err != nil {
			return err
		}
		resp, err := do(client, req)
		if err != nil {
			return err
		}
		if err := statusError(resp); err != nil {
			resp.Body.Close()
			return err
		}
		r = resp.Body
	}
	defer r.Close()
	n, err := io.Copy(w, io.LimitReader(readMarker{r}, rel.Size+1))
	var rerr readError
	if errors.As(err, &rerr) && rel.src.Dir == "" {
		// The connection broke or stalled: the source out of reach.
		return &unreachableError{fmt.Errorf("download update: %w", rerr.error)}
	}
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	if n != rel.Size {
		return fmt.Errorf("download update: got %d bytes of %d", n, rel.Size)
	}
	return nil
}

// fileSHA256 is the hex SHA-256 of the file at path.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FileSHA256 is exported for the release tool.
func FileSHA256(path string) (string, error) { return fileSHA256(path) }
