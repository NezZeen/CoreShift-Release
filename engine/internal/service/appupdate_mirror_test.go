package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"coreshift/engine/internal/selfupdate"
	"coreshift/engine/internal/store"
)

// mirrorFake answers checks by source: GitHub's answer or error, and the
// mirror's; it counts what each was asked.
type mirrorFake struct {
	mu        sync.Mutex
	github    error
	githubRel selfupdate.Manifest
	gitlabRel selfupdate.Manifest
	asked     map[string]int
	// downloadErr fails downloads of releases found on GitHub.
	downloadErr error
	downloaded  []string
}

func (f *mirrorFake) install(c *Config) {
	c.checkRelease = func(_ context.Context, _ *http.Client, src selfupdate.Source) (selfupdate.Release, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.asked[src.Site()]++
		if src.GitLab {
			return selfupdate.Release{Manifest: f.gitlabRel, Page: "gitlab"}, nil
		}
		if f.github != nil {
			return selfupdate.Release{}, f.github
		}
		return selfupdate.Release{Manifest: f.githubRel, Page: "github"}, nil
	}
	c.downloadRelease = func(_ context.Context, _ *http.Client, rel selfupdate.Release, dir string) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.downloaded = append(f.downloaded, rel.Page)
		if rel.Page == "github" && f.downloadErr != nil {
			return "", f.downloadErr
		}
		return filepath.Join(dir, rel.Installer), nil
	}
}

// updateLines are the journal's lines about where the update came from.
func updateLines(h *harness) []string {
	var lines []string
	for len(h.events) > 0 {
		if e := <-h.events; e.Kind == "action" && e.Source == "обновления" {
			lines = append(lines, e.Line)
		}
	}
	return lines
}

// GitHub out of reach: the mirror on GitLab is asked, and the journal says
// so. What GitHub answers, though, is the answer.
func TestUpdateMirrorFallback(t *testing.T) {
	newer := selfupdate.Manifest{Version: "9.0.0", Build: 5, Installer: "CoreShift-Setup.exe", SHA256: strings.Repeat("a", 64), Size: 10}
	older := selfupdate.Manifest{Version: "0.0.0", Build: 0, Installer: "CoreShift-Setup.exe", SHA256: strings.Repeat("b", 64), Size: 10}
	for _, c := range []struct {
		name      string
		github    error
		wantFrom  string
		wantAsked int // of GitLab
		wantErr   string
	}{
		{"unreachable", selfupdate.MarkUnreachable(errors.New("check for updates: dial tcp: i/o timeout")), "gitlab", 1, ""},
		{"rate limited", selfupdate.MarkUnreachable(errors.New("check for updates: GitHub rate limit, try again later")), "gitlab", 1, ""},
		{"bad signature", errors.New("update signature does not match the release key"), "", 0, "signature"},
		{"no release", errors.New("check for updates: no release has latest.json"), "", 0, "no release has"},
		{"not newer", nil, "github", 0, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &mirrorFake{github: c.github, githubRel: older, gitlabRel: newer, asked: map[string]int{}}
			h := newHarness(t, f.install)
			srcs, err := h.svc.updateSources()
			if err != nil || len(srcs) != 2 || srcs[0].String() != selfupdate.PublicSource || srcs[1].String() != selfupdate.MirrorSource {
				t.Fatalf("sources %v, %v", srcs, err)
			}
			updateLines(h)
			rel, from, err := h.svc.findRelease(context.Background(), srcs)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("err = %v", err)
				}
			} else if err != nil || rel.Page != c.wantFrom || (from == 1) != (c.wantFrom == "gitlab") {
				t.Errorf("found %q from %d, %v", rel.Page, from, err)
			}
			if f.asked["GitLab"] != c.wantAsked || f.asked["GitHub"] != 1 {
				t.Errorf("asked %v", f.asked)
			}
			lines := updateLines(h)
			if c.wantFrom == "gitlab" {
				if len(lines) != 1 || lines[0] != "GitHub недоступен, проверено через GitLab" {
					t.Errorf("journal %q", lines)
				}
			} else if len(lines) != 0 {
				t.Errorf("journal %q", lines)
			}
		})
	}
}

// Both out of reach: the error tells both.
func TestUpdateMirrorAlsoUnreachable(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.checkRelease = func(_ context.Context, _ *http.Client, src selfupdate.Source) (selfupdate.Release, error) {
			return selfupdate.Release{}, selfupdate.MarkUnreachable(fmt.Errorf("check for updates: %s timeout", src.Site()))
		}
	})
	srcs, _ := h.svc.updateSources()
	_, _, err := h.svc.findRelease(context.Background(), srcs)
	if err == nil || !strings.Contains(err.Error(), "GitHub timeout") || !strings.Contains(err.Error(), "GitLab: check for updates: GitLab timeout") {
		t.Errorf("err = %v", err)
	}
}

// The source the user set is the only one asked.
func TestUpdateSourceSetting(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set := st.Settings()
	set.AppUpdate.Source = "github-public:o/r"
	if _, err := st.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	f := &mirrorFake{github: selfupdate.MarkUnreachable(errors.New("check for updates: dial tcp: i/o timeout")), asked: map[string]int{}}
	h := newHarness(t, func(c *Config) { f.install(c); c.Store = st })
	srcs, err := h.svc.updateSources()
	if err != nil || len(srcs) != 1 || srcs[0].String() != "github-public:o/r" {
		t.Fatalf("sources %v, %v", srcs, err)
	}
	if _, _, err := h.svc.findRelease(context.Background(), srcs); err == nil || f.asked["GitLab"] != 0 {
		t.Errorf("asked %v, %v", f.asked, err)
	}
}

// Found on GitHub, but its download out of reach: the same release from
// the mirror. A different release there is not taken.
func TestUpdateDownloadFromMirror(t *testing.T) {
	rel := selfupdate.Manifest{Version: "9.0.0", Build: 5, Installer: "CoreShift-Setup.exe", SHA256: strings.Repeat("a", 64), Size: 10}
	f := &mirrorFake{githubRel: rel, gitlabRel: rel, asked: map[string]int{},
		downloadErr: selfupdate.MarkUnreachable(errors.New("download update: connection reset"))}
	h := newHarness(t, f.install)
	srcs, _ := h.svc.updateSources()
	found, from, err := h.svc.findRelease(context.Background(), srcs)
	if err != nil || from != 0 {
		t.Fatal(from, err)
	}
	updateLines(h)
	if _, err := h.svc.downloadUpdate(context.Background(), srcs[from:], found, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.downloaded, []string{"github", "gitlab"}) {
		t.Errorf("downloaded %v", f.downloaded)
	}
	if lines := updateLines(h); len(lines) != 1 || lines[0] != "GitHub недоступен, обновление скачано через GitLab" {
		t.Errorf("journal %q", lines)
	}

	f.downloaded = nil
	f.gitlabRel.SHA256 = strings.Repeat("c", 64)
	if _, err := h.svc.downloadUpdate(context.Background(), srcs[from:], found, t.TempDir()); err == nil || !slices.Equal(f.downloaded, []string{"github"}) {
		t.Errorf("another release taken: %v, %v", f.downloaded, err)
	}
	// A checksum that does not match is no reason to look elsewhere.
	f.downloaded, f.gitlabRel = nil, rel
	f.downloadErr = errors.New("download update: checksum mismatch (CoreShift-Setup.exe)")
	if _, err := h.svc.downloadUpdate(context.Background(), srcs[from:], found, t.TempDir()); err == nil || len(f.downloaded) != 1 {
		t.Errorf("after a bad checksum: %v, %v", f.downloaded, err)
	}
}
