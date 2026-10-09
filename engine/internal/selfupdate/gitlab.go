package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// The mirror of the releases on GitLab (MirrorSource): publish.ps1 puts the
// same files in the project's generic package registry and makes a release
// whose asset links point at them. GitLab's links carry no size, so the
// installer's size and SHA-256 are only checked while it downloads
// (Download), against the signed manifest.
//
// Nothing goes to GitLab but plain requests to its own host: no token
// (the project is public), and no address from its answers that is not
// on that host. A download may be redirected by GitLab itself to its
// object storage, as GitHub's are; the manifest's signature and the
// installer's SHA-256 make the file trusted, not where it came from.

// GitLabBase is gitlab.com; tests point it at a local server.
var GitLabBase = "https://gitlab.com"

// gitlabWeb is where the user opens a release page (Linux announces).
const gitlabWeb = "https://gitlab.com"

func checkGitLab(ctx context.Context, client *http.Client, src Source, manifest string, keys []string) (Release, error) {
	links, page, err := gitlabLinks(ctx, client, src, manifest)
	if err != nil {
		return Release{}, err
	}
	link := func(name string) (string, error) {
		u, ok := links[name]
		if !ok {
			return "", fmt.Errorf("check for updates: the latest release on GitLab has no %s", name)
		}
		if u == "" {
			return "", fmt.Errorf("check for updates: GitLab links %s to an unexpected address", name)
		}
		return u, nil
	}
	files := map[string][]byte{}
	for _, name := range []string{manifest, manifest + ".sig"} {
		u, err := link(name)
		if err != nil {
			return Release{}, err
		}
		body, err := gitlabOpen(ctx, client, u)
		if err != nil {
			return Release{}, err
		}
		files[name], err = readLimited(body, 1<<20)
		body.Close()
		if err != nil {
			return Release{}, err
		}
	}
	m, err := Verify(files[manifest], files[manifest+".sig"], keys)
	if err != nil {
		return Release{}, err
	}
	installer, err := link(m.Installer)
	if err != nil {
		return Release{}, err
	}
	return Release{Manifest: m, Page: page, src: src, installer: installer}, nil
}

type gitlabLink struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Direct string `json:"direct_asset_url"`
}

// gitlabLinks returns the asset links of the newest release that has the
// file manifest, by name, and its page. A link to an address it may not
// fetch (gitlabAssetURL) is there as "".
func gitlabLinks(ctx context.Context, client *http.Client, src Source, manifest string) (map[string]string, string, error) {
	// Newest first: GitLab sorts by released_at, descending.
	body, err := gitlabOpen(ctx, client, GitLabBase+"/api/v4/projects/"+url.PathEscape(src.Repo)+"/releases?per_page=30")
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	var releases []struct {
		Upcoming bool `json:"upcoming_release"`
		Links    struct {
			Self string `json:"self"`
		} `json:"_links"`
		Assets struct {
			Links []gitlabLink `json:"links"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 8<<20)).Decode(&releases); err != nil {
		return nil, "", decodeError(err)
	}
	for _, rel := range releases {
		// Released in the future: not out yet.
		if rel.Upcoming {
			continue
		}
		out := map[string]string{}
		for _, l := range rel.Assets.Links {
			out[l.Name] = gitlabAssetURL(src, l)
		}
		if _, ok := out[manifest]; ok {
			return out, gitlabReleasePage(src.Repo, rel.Links.Self), nil
		}
	}
	return nil, "", fmt.Errorf("check for updates: no release has %s (GitLab)", manifest)
}

// A file of the generic package registry: /api/v4/projects/<id or encoded
// path>/packages/generic/<package>/<version>/<file>.
var gitlabPackageRE = regexp.MustCompile(`^/api/v4/projects/[A-Za-z0-9%._-]+/packages/generic/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// gitlabAssetURL is the address a link's file is fetched from: its package
// registry URL, else its release download (/<path>/-/releases/<tag>/
// downloads/<file>), on GitLab's own host. "" when neither is.
func gitlabAssetURL(src Source, l gitlabLink) string {
	base, err := url.Parse(GitLabBase)
	if err != nil {
		return ""
	}
	for _, raw := range []string{l.URL, l.Direct} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
			strings.Contains(u.Path, "..") {
			continue
		}
		p := u.EscapedPath()
		download := "/" + src.Repo + "/-/releases/"
		if gitlabPackageRE.MatchString(p) || strings.HasPrefix(p, download) && strings.Contains(p[len(download):], "/downloads/") {
			return u.String()
		}
	}
	return ""
}

// gitlabOpen fetches rawURL, which must be on GitLab's host, without any
// credentials, and returns its body.
func gitlabOpen(ctx context.Context, client *http.Client, rawURL string) (io.ReadCloser, error) {
	base, err := url.Parse(GitLabBase)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil {
		return nil, errors.New("check for updates: unexpected address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, application/octet-stream")
	req.Header.Set("User-Agent", "CoreShift")
	resp, err := do(client, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, transient(resp.StatusCode, gitlabStatusError(resp))
	}
	return resp.Body, nil
}

func gitlabStatusError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusNotFound:
		return errors.New("check for updates: no such project or file on GitLab (404)")
	case http.StatusTooManyRequests:
		return errors.New("check for updates: GitLab rate limit, try again later")
	}
	return fmt.Errorf("check for updates: GitLab returned %s", resp.Status)
}

// gitlabReleasePage keeps page only when it is a release page of the
// project path on gitlab.com, else gives the project's releases: the
// address comes unsigned from the API, and the user opens it.
func gitlabReleasePage(path, page string) string {
	prefix := "/" + path + "/-/releases/"
	u, err := url.Parse(page)
	if err != nil || u.Scheme != "https" || u.Host != "gitlab.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		!strings.HasPrefix(u.Path, prefix) || len(u.Path) == len(prefix) || strings.Contains(u.Path, "..") {
		return gitlabWeb + "/" + path + "/-/releases"
	}
	return gitlabWeb + u.EscapedPath()
}
