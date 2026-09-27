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

// token reads the releases of the private repository. Release builds set it
// in a generated, ignored file (token_gen.go, see packaging/windows/
// build.ps1); without it only folder sources work.
var token string

// HasToken reports whether this build can read the private releases.
func HasToken() bool { return token != "" }

// Source is where releases come from: a GitHub repository or a folder.
type Source struct {
	Repo string // OWNER/REPO
	Dir  string
}

var repoRE = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

// ParseSource accepts "github:OWNER/REPO" or an absolute folder path.
func ParseSource(s string) (Source, error) {
	s = strings.TrimSpace(s)
	if repo, ok := strings.CutPrefix(s, "github:"); ok {
		if !repoRE.MatchString(repo) {
			return Source{}, fmt.Errorf("%q is not github:OWNER/REPO", s)
		}
		return Source{Repo: repo}, nil
	}
	if filepath.IsAbs(s) {
		return Source{Dir: filepath.Clean(s)}, nil
	}
	return Source{}, fmt.Errorf("%q is neither github:OWNER/REPO nor a full folder path", s)
}

func (s Source) String() string {
	if s.Repo != "" {
		return "github:" + s.Repo
	}
	return s.Dir
}

// Release is a verified manifest and where its installer is.
type Release struct {
	Manifest
	src       Source
	installer string // the asset's API URL, or the file's path
}

// Check reads and verifies the manifest named manifest (see ManifestFor)
// of the latest release.
func Check(ctx context.Context, client *http.Client, src Source, manifest string, keys []string) (Release, error) {
	var rel Release
	var err error
	if src.Dir != "" {
		rel, err = checkDir(src, manifest, keys)
	} else {
		rel, err = checkGitHub(ctx, client, src, manifest, keys)
	}
	if err == nil && filepath.Ext(rel.Installer) != installerExt(manifest) {
		return Release{}, fmt.Errorf("update manifest: %s names %s, not a %s file", manifest, rel.Installer, installerExt(manifest))
	}
	return rel, err
}

func checkDir(src Source, manifest string, keys []string) (Release, error) {
	body, err := os.ReadFile(filepath.Join(src.Dir, manifest))
	if err != nil {
		return Release{}, fmt.Errorf("check for updates: %w", err)
	}
	sig, err := os.ReadFile(filepath.Join(src.Dir, manifest+".sig"))
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
	assets, err := releaseAssets(ctx, client, src.Repo, manifest)
	if err != nil {
		return Release{}, err
	}
	files := map[string][]byte{}
	for _, name := range []string{manifest, manifest + ".sig"} {
		a, ok := assets[name]
		if !ok {
			return Release{}, fmt.Errorf("check for updates: the latest release has no %s", name)
		}
		if files[name], err = fetchAsset(ctx, client, a.URL, 1<<20); err != nil {
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
	return Release{Manifest: m, src: src, installer: a.URL}, nil
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// releaseAssets returns the assets of the newest published release that has
// the file manifest: a release for one platform does not hide the previous
// one of the other.
func releaseAssets(ctx context.Context, client *http.Client, repo, manifest string) (map[string]asset, error) {
	if token == "" {
		return nil, errors.New("this build has no token for the private releases")
	}
	// Newest first.
	req, err := githubRequest(ctx, APIBase+"/repos/"+repo+"/releases?per_page=30", "application/vnd.github+json")
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
	var releases []struct {
		Draft      bool    `json:"draft"`
		Prerelease bool    `json:"prerelease"`
		Assets     []asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("check for updates: %w", err)
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
			return out, nil
		}
	}
	return nil, fmt.Errorf("check for updates: no release has %s", manifest)
}

// githubRequest adds the token. The asset download redirects to another
// host, and net/http does not forward Authorization there.
func githubRequest(ctx context.Context, rawURL, accept string) (*http.Request, error) {
	// The token goes to GitHub's API only; asset URLs come from its answers.
	if !strings.HasPrefix(rawURL, APIBase+"/") {
		return nil, errors.New("check for updates: unexpected address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "CoreShift")
	return req, nil
}

// do runs req, keeping addresses out of the error: asset downloads redirect
// to signed URLs, which are credentials for a while.
func do(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("check for updates: %w", err)
	}
	return resp, nil
}

func statusError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errors.New("check for updates: the releases token is invalid or expired (401)")
	case http.StatusNotFound:
		// A private repository the token cannot see looks missing.
		return errors.New("check for updates: no release found, or the token has no access (404)")
	}
	return fmt.Errorf("check for updates: server returned %s", resp.Status)
}

func fetchAsset(ctx context.Context, client *http.Client, apiURL string, limit int64) ([]byte, error) {
	req, err := githubRequest(ctx, apiURL, "application/octet-stream")
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
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("check for updates: %w", err)
	}
	if int64(len(b)) > limit {
		return nil, errors.New("check for updates: file too large")
	}
	return b, nil
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
		f, err := os.Open(rel.installer)
		if err != nil {
			return fmt.Errorf("download update: %w", err)
		}
		r = f
	} else {
		req, err := githubRequest(ctx, rel.installer, "application/octet-stream")
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
	n, err := io.Copy(w, io.LimitReader(r, rel.Size+1))
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
