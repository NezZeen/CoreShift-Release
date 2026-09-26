// Package coreupdate finds the latest release of a proxy core on GitHub and
// installs it over the running one.
//
// A running executable cannot be overwritten on Windows, but it can be
// renamed: the old file becomes "<name>.old" and the new one takes its
// place, so a core that is running keeps running and the next start uses
// the new version. Cleanup removes the leftovers later.
package coreupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"coreshift/engine/internal/core"
)

// APIBase is the GitHub API; tests point it elsewhere.
var APIBase = "https://api.github.com"

const maxDownload = 256 << 20

var repos = map[core.Kind]string{
	core.Xray:    "XTLS/Xray-core",
	core.SingBox: "SagerNet/sing-box",
	core.Mihomo:  "MetaCubeX/mihomo",
}

// Release is the latest stable release of a core for this platform.
type Release struct {
	Kind    core.Kind `json:"kind"`
	Tag     string    `json:"tag"`
	Version string    `json:"version"` // Tag without the leading "v"
	Asset   string    `json:"asset"`
	URL     string    `json:"-"`
	Size    int64     `json:"size"`
	SHA256  string    `json:"-"`
}

// assetName is the release file for goos/goarch, as each project names them.
func assetName(k core.Kind, tag, goos, goarch string) (string, error) {
	ver := strings.TrimPrefix(tag, "v")
	ext := ".zip"
	switch k {
	case core.Xray:
		arch := map[string]string{"amd64": "64", "arm64": "arm64-v8a"}[goarch]
		if arch == "" || (goos != "windows" && goos != "linux") {
			break
		}
		return fmt.Sprintf("Xray-%s-%s.zip", goos, arch), nil
	case core.SingBox:
		if goos == "linux" {
			ext = ".tar.gz"
		}
		if (goos == "windows" || goos == "linux") && (goarch == "amd64" || goarch == "arm64") {
			return fmt.Sprintf("sing-box-%s-%s-%s%s", ver, goos, goarch, ext), nil
		}
	case core.Mihomo:
		if goos == "linux" {
			ext = ".gz"
		}
		if goos != "windows" && goos != "linux" {
			break
		}
		switch goarch {
		case "amd64":
			// v1 runs on any x86-64 processor.
			return fmt.Sprintf("mihomo-%s-amd64-v1-v%s%s", goos, ver, ext), nil
		case "arm64":
			return fmt.Sprintf("mihomo-%s-arm64-v%s%s", goos, ver, ext), nil
		}
	}
	return "", fmt.Errorf("%s: no release build for %s/%s", k, goos, goarch)
}

// Latest asks GitHub for the newest stable release of k.
func Latest(ctx context.Context, c *http.Client, k core.Kind) (Release, error) {
	repo, ok := repos[k]
	if !ok {
		return Release{}, fmt.Errorf("unknown core %q", k)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBase+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CoreShift")
	resp, err := c.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("%s: check for updates: %w", k, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("%s: check for updates: GitHub answered %s", k, resp.Status)
	}
	var body struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("%s: check for updates: %w", k, err)
	}
	name, err := assetName(k, body.Tag, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Release{}, err
	}
	for _, a := range body.Assets {
		if a.Name != name {
			continue
		}
		sum, ok := strings.CutPrefix(a.Digest, "sha256:")
		if !ok || len(sum) != 64 {
			return Release{}, fmt.Errorf("%s %s: GitHub gives no SHA-256 for %s", k, body.Tag, name)
		}
		return Release{Kind: k, Tag: body.Tag, Version: strings.TrimPrefix(body.Tag, "v"), Asset: name, URL: a.URL, Size: a.Size, SHA256: sum}, nil
	}
	return Release{}, fmt.Errorf("%s %s: release has no %s", k, body.Tag, name)
}

// Install downloads rel, checks its SHA-256, and puts its executable in
// place of bin, together with the libraries next to bin that the release
// ships as well (sing-box's libcronet.dll). The new core must report its
// version, or everything is put back. It returns that version.
func Install(ctx context.Context, c *http.Client, rel Release, bin string) (string, error) {
	archive, err := download(ctx, c, rel, filepath.Dir(bin))
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)

	files, err := extract(archive, rel, bin)
	if err != nil {
		return "", fmt.Errorf("%s: unpack %s: %w", rel.Kind, rel.Asset, err)
	}
	defer func() {
		for _, f := range files {
			os.Remove(f.tmp)
		}
	}()
	undo, err := swap(files)
	if err != nil {
		return "", fmt.Errorf("%s: install: %w", rel.Kind, err)
	}
	v, err := core.Version(ctx, rel.Kind, bin)
	if err != nil {
		undo()
		return "", fmt.Errorf("%s: the new version does not start, kept the old one: %w", rel.Kind, err)
	}
	return v, nil
}

func download(ctx context.Context, c *http.Client, rel Release, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "CoreShift")
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: download: %w", rel.Kind, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: download: %s", rel.Kind, resp.Status)
	}
	f, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxDownload+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		err = fmt.Errorf("%s: download: %w", rel.Kind, err)
	case n > maxDownload:
		err = fmt.Errorf("%s: download: larger than %d MB", rel.Kind, maxDownload>>20)
	case rel.Size > 0 && n != rel.Size:
		err = fmt.Errorf("%s: download: got %d bytes of %d", rel.Kind, n, rel.Size)
	case !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), rel.SHA256):
		err = fmt.Errorf("%s: download: checksum mismatch", rel.Kind)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// file is one file to install: tmp is unpacked, dst is where it goes.
type file struct {
	tmp, dst string
}

// isExe reports whether an archive entry is k's executable.
func isExe(k core.Kind, name string) bool {
	base := strings.ToLower(pathBase(name))
	if runtime.GOOS == "windows" {
		var ok bool
		if base, ok = strings.CutSuffix(base, ".exe"); !ok {
			return false
		}
	} else if strings.Contains(base, ".") {
		return false
	}
	switch k {
	case core.Xray:
		return base == "xray"
	case core.SingBox:
		return base == "sing-box"
	case core.Mihomo:
		return strings.HasPrefix(base, "mihomo")
	}
	return false
}

func pathBase(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	return name[strings.LastIndex(name, "/")+1:]
}

func pathDir(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	return name[:strings.LastIndex(name, "/")+1]
}

func extract(archive string, rel Release, bin string) ([]file, error) {
	dir := filepath.Dir(bin)
	var out []file
	write := func(r io.Reader, dst string) error {
		f, err := os.CreateTemp(dir, ".new-*")
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(r, maxDownload))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(f.Name(), 0o755)
		}
		out = append(out, file{tmp: f.Name(), dst: dst})
		return err
	}
	// companion is a library the release ships next to the executable that
	// is also installed next to bin.
	companion := func(name, exeDir string) (string, bool) {
		if pathDir(name) != exeDir || !strings.EqualFold(filepath.Ext(name), ".dll") {
			return "", false
		}
		dst := filepath.Join(dir, pathBase(name))
		_, err := os.Stat(dst)
		return dst, err == nil
	}

	switch {
	case strings.HasSuffix(rel.Asset, ".zip"):
		z, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		defer z.Close()
		exeDir := ""
		var exe *zip.File
		for _, f := range z.File {
			if !f.FileInfo().IsDir() && isExe(rel.Kind, f.Name) {
				exe, exeDir = f, pathDir(f.Name)
				break
			}
		}
		if exe == nil {
			return nil, errors.New("no executable in the archive")
		}
		for _, f := range z.File {
			dst, ok := bin, f == exe
			if !ok {
				dst, ok = companion(f.Name, exeDir)
			}
			if !ok {
				continue
			}
			r, err := f.Open()
			if err != nil {
				return out, err
			}
			err = write(r, dst)
			r.Close()
			if err != nil {
				return out, err
			}
		}
		return out, nil
	case strings.HasSuffix(rel.Asset, ".tar.gz"):
		f, err := os.Open(archive)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return nil, errors.New("no executable in the archive")
			}
			if err != nil {
				return nil, err
			}
			if h.Typeflag == tar.TypeReg && isExe(rel.Kind, h.Name) {
				return out, write(tr, bin)
			}
		}
	case strings.HasSuffix(rel.Asset, ".gz"):
		// A single compressed executable (mihomo on Linux).
		f, err := os.Open(archive)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		return out, write(gz, bin)
	}
	return nil, fmt.Errorf("unknown archive type %s", rel.Asset)
}

// swap puts every file in place, keeping the old ones as .old. On failure
// it puts back what it already moved; undo does the same after success.
func swap(files []file) (undo func(), err error) {
	type moved struct{ dst, old string }
	var done []moved
	undo = func() {
		for i := len(done) - 1; i >= 0; i-- {
			m := done[i]
			if m.old == "" {
				os.Remove(m.dst)
				continue
			}
			// The new file may be running already; move it aside first.
			aside := m.dst + ".failed"
			os.Remove(aside)
			if os.Rename(m.dst, aside) == nil {
				os.Remove(aside)
			}
			_ = os.Rename(m.old, m.dst)
		}
	}
	for _, f := range files {
		old := f.dst + ".old"
		os.Remove(old) // a leftover from an earlier update, unless still running
		if _, err := os.Stat(f.dst); err == nil {
			if err := os.Rename(f.dst, old); err != nil {
				undo()
				return nil, err
			}
		} else {
			old = ""
		}
		if err := os.Rename(f.tmp, f.dst); err != nil {
			if old != "" {
				_ = os.Rename(old, f.dst)
			}
			undo()
			return nil, err
		}
		done = append(done, moved{dst: f.dst, old: old})
	}
	return undo, nil
}

// Cleanup removes what earlier updates left next to the executables: old
// versions that were still running then, and unfinished downloads.
func Cleanup(bins map[core.Kind]string) {
	seen := map[string]bool{}
	for _, bin := range bins {
		dir := filepath.Dir(bin)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		for _, pattern := range []string{"*.old", "*.failed", ".download-*", ".new-*"} {
			matches, _ := filepath.Glob(filepath.Join(dir, pattern))
			for _, m := range matches {
				os.Remove(m)
			}
		}
	}
}
