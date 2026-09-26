package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/coreupdate"
)

// coreState caches core versions, which take a process start to learn,
// and serialises updates.
type coreState struct {
	mu       sync.Mutex
	versions map[core.Kind]string
	updating sync.Mutex
}

// CoreVersions returns the version of every installed core; a core that
// does not tell is left out.
func (s *Service) CoreVersions(ctx context.Context) map[core.Kind]string {
	s.cores.mu.Lock()
	if s.cores.versions == nil {
		s.cores.versions = map[core.Kind]string{}
	}
	missing := map[core.Kind]string{}
	for k, bin := range s.cfg.Binaries {
		if _, ok := s.cores.versions[k]; !ok {
			missing[k] = bin
		}
	}
	s.cores.mu.Unlock()

	var wg sync.WaitGroup
	for k, bin := range missing {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := core.Version(ctx, k, bin)
			if err != nil {
				return
			}
			s.cores.mu.Lock()
			s.cores.versions[k] = v
			s.cores.mu.Unlock()
		}()
	}
	wg.Wait()

	s.cores.mu.Lock()
	defer s.cores.mu.Unlock()
	out := make(map[core.Kind]string, len(s.cores.versions))
	for k, v := range s.cores.versions {
		out[k] = v
	}
	return out
}

// CoreUpdate says whether a newer release of an installed core exists.
type CoreUpdate struct {
	Kind      core.Kind `json:"kind"`
	Current   string    `json:"current,omitempty"`
	Latest    string    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	// Size is the download in bytes.
	Size  int64  `json:"size,omitempty"`
	Error string `json:"error,omitempty"`
}

// CheckCoreUpdates asks GitHub for the latest release of every installed
// core.
func (s *Service) CheckCoreUpdates(ctx context.Context) []CoreUpdate {
	versions := s.CoreVersions(ctx)
	var out []CoreUpdate
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, a := range core.Adapters() {
		k := a.Kind()
		if s.cfg.Binaries[k] == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := CoreUpdate{Kind: k, Current: versions[k]}
			var rel coreupdate.Release
			err := s.viaProxyOrDirect(ctx, 30*time.Second, func(c *http.Client) (err error) {
				rel, err = coreupdate.Latest(ctx, c, k)
				return err
			})
			if err != nil {
				u.Error = err.Error()
			} else {
				u.Latest, u.Size = rel.Version, rel.Size
				u.Available = u.Current == "" || newerVersion(rel.Version, u.Current)
			}
			mu.Lock()
			out = append(out, u)
			mu.Unlock()
		}()
	}
	wg.Wait()
	// In priority order, as the UI lists cores.
	sorted := make([]CoreUpdate, 0, len(out))
	for _, a := range core.Adapters() {
		for _, u := range out {
			if u.Kind == a.Kind() {
				sorted = append(sorted, u)
			}
		}
	}
	return sorted
}

// UpdateCore installs the latest release of k over the current one and
// returns the new version. A running core keeps its old version until the
// next connection.
func (s *Service) UpdateCore(ctx context.Context, k core.Kind) (string, error) {
	bin := s.cfg.Binaries[k]
	if bin == "" {
		return "", fmt.Errorf("%s is not installed", k)
	}
	if !s.cores.updating.TryLock() {
		return "", errors.New("another core is being updated")
	}
	defer s.cores.updating.Unlock()

	var rel coreupdate.Release
	if err := s.viaProxyOrDirect(ctx, 30*time.Second, func(c *http.Client) (err error) {
		rel, err = coreupdate.Latest(ctx, c, k)
		return err
	}); err != nil {
		return "", err
	}
	var v string
	if err := s.viaProxyOrDirect(ctx, 10*time.Minute, func(c *http.Client) (err error) {
		v, err = coreupdate.Install(ctx, c, rel, bin)
		return err
	}); err != nil {
		return "", err
	}
	s.cores.mu.Lock()
	if s.cores.versions == nil {
		s.cores.versions = map[core.Kind]string{}
	}
	s.cores.versions[k] = v
	s.cores.mu.Unlock()

	s.mu.Lock()
	active := s.status.State == Connected || s.status.State == Connecting
	s.pending = s.pending || active
	s.mu.Unlock()
	s.hub.publish(Event{Kind: "cores", Core: string(k), Reason: "updated", Line: v})
	if active {
		s.hub.publish(Event{Kind: "options"})
	}
	return v, nil
}

// viaProxyOrDirect runs do through the active core first, as GitHub may be
// blocked where the user is, then directly.
func (s *Service) viaProxyOrDirect(ctx context.Context, timeout time.Duration, do func(*http.Client) error) error {
	var clients []*http.Client
	if st := s.Status().State; st == Connected {
		proxy := &url.URL{Scheme: "socks5", Host: s.cfg.Listen.String()}
		clients = append(clients, &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}})
	}
	clients = append(clients, &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}})
	var err error
	for _, c := range clients {
		if err = do(c); err == nil || ctx.Err() != nil {
			return err
		}
	}
	return err
}

// newerVersion reports whether a is a later version than b, comparing the
// numbers of "1.14.2"; a pre-release suffix counts as older than none.
func newerVersion(a, b string) bool {
	na, pa := splitVersion(a)
	nb, pb := splitVersion(b)
	for i := 0; i < max(len(na), len(nb)); i++ {
		var x, y int
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			return x > y
		}
	}
	return pa == "" && pb != ""
}

func splitVersion(v string) (nums []int, pre string) {
	v = strings.TrimPrefix(v, "v")
	v, pre, _ = strings.Cut(v, "-")
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		nums = append(nums, n)
	}
	return nums, pre
}
