package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
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
	// probing is closed when the cores being asked for their versions have
	// answered; nil when none are.
	probing  chan struct{}
	updating sync.Mutex
	// stale holds the updates installed while a connection may run the
	// old version; applying is the connection whose applyCores runs, 0
	// for none (coreapply.go).
	stale map[core.Kind]staleCore
	// tun is an update of sing-box the TUN layer, a sing-box process of its
	// own on the desktop, may still run the old version of; nil for none.
	tun      *staleCore
	applying int
}

// CoreVersions returns the version of every installed core; a core that
// does not tell is left out. Callers at the same time share one start of
// each core: the app asks as it opens, just as the start asks
// (WarmUp).
func (s *Service) CoreVersions(ctx context.Context) map[core.Kind]string {
	s.cores.mu.Lock()
	if s.cores.versions == nil {
		s.cores.versions = map[core.Kind]string{}
	}
	if done := s.cores.probing; done != nil {
		s.cores.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		}
		return s.knownVersions()
	}
	missing := map[core.Kind]string{}
	for k, bin := range s.cfg.Binaries {
		if _, ok := s.cores.versions[k]; !ok {
			missing[k] = bin
		}
	}
	if len(missing) == 0 {
		s.cores.mu.Unlock()
		return s.knownVersions()
	}
	done := make(chan struct{})
	s.cores.probing = done
	s.cores.mu.Unlock()

	var wg sync.WaitGroup
	for k, bin := range missing {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.cfg.coreVersion(ctx, k, bin)
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
	s.cores.probing = nil
	s.cores.mu.Unlock()
	close(done)
	return s.knownVersions()
}

func (s *Service) knownVersions() map[core.Kind]string {
	s.cores.mu.Lock()
	defer s.cores.mu.Unlock()
	return maps.Clone(s.cores.versions)
}

// WarmUp learns what the app asks for first as it opens, so the answer is
// ready by then: the cores' versions, which take starting each core (a
// quarter of a second on a PC, more on a phone). Run it on its own
// goroutine at start.
func (s *Service) WarmUp(ctx context.Context) { s.CoreVersions(ctx) }

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
// connection is quiet for a while, then moves to the new one without
// disconnecting (coreapply.go); or until it starts again anyway.
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
	// Never an older version, nor the same one again: a release the
	// project took back, or an answer that is not GitHub's, would
	// otherwise replace a newer core.
	if cur := s.CoreVersions(ctx)[k]; cur != "" && !newerVersion(rel.Version, cur) {
		return "", fmt.Errorf("%s %s is installed; the latest release, %s, is not newer", k, cur, rel.Version)
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

	s.hub.publish(Event{Kind: "cores", Core: string(k), Reason: "updated", Line: v})
	s.noteInstalled(k, v)
	return v, nil
}

// settleWait is how long viaProxyOrDirect waits for a connection that is
// coming up or going down.
const settleWait = 30 * time.Second

// viaProxyOrDirect runs do through the active core first, as GitHub may be
// blocked where the user is, then directly. Each client's connections are
// closed when it is done. When both fail, the error tells both reasons: a
// refusal through the proxy (GitHub's rate limit on a shared server
// address) is not lost behind the direct attempt's.
//
// A connection that is coming up or going down is waited for first: the
// app checks the cores' updates as it starts, which is when the service
// connects by itself. Chosen mid-way, the proxy stops under the request,
// and a direct request made before the TUN layer is up is cut off by its
// strict routes; every check then failed as "no connection to GitHub".
//
// Without a connection the one direct attempt's error is untunneled, for
// retryAfterConnect.
func (s *Service) viaProxyOrDirect(ctx context.Context, timeout time.Duration, do func(*http.Client) error) error {
	return s.via(ctx, timeout, false, do)
}

// viaDirectOrProxy is viaProxyOrDirect the other way round, for
// subscriptions: directly first, then through the active core. Panels
// often refuse VPN servers' addresses (their own nodes, hosting ranges) or
// count them against the device limit, and the direct way is the one the
// subscription was added with. The core is the way out when the network
// lets nothing else through: a mobile operator's white list resets every
// direct connection to an address not on it, and some networks do not
// resolve the panel's name.
func (s *Service) viaDirectOrProxy(ctx context.Context, timeout time.Duration, do func(*http.Client) error) error {
	return s.via(ctx, timeout, true, do)
}

func (s *Service) via(ctx context.Context, timeout time.Duration, directFirst bool, do func(*http.Client) error) error {
	s.waitSettled(ctx, settleWait)
	proxies := []*url.URL{nil}
	if st := s.Status().State; st == Connected {
		proxies = []*url.URL{s.proxyURL(), nil}
		if directFirst {
			proxies = []*url.URL{nil, s.proxyURL()}
		}
	}
	var errs []error
	for _, p := range proxies {
		tr := newTransport(p)
		if p == nil && s.cfg.dialDirect != nil {
			tr.DialContext = s.cfg.dialDirect
		}
		err := do(&http.Client{Timeout: timeout, Transport: tr})
		tr.CloseIdleConnections()
		if err == nil {
			return nil
		}
		if len(proxies) > 1 {
			how := "directly"
			if p != nil {
				how = "through the proxy"
			}
			err = fmt.Errorf("%s: %w", how, err)
		}
		errs = append(errs, err)
		if ctx.Err() != nil {
			break
		}
	}
	if len(errs) == 2 {
		return fmt.Errorf("%w; %w", errs[0], errs[1]) // on one line, for the journal
	}
	if len(proxies) == 1 {
		return &untunneled{errs[0]}
	}
	return errs[0]
}

// waitSettled waits, at most max, while a connection is coming up or going
// down.
func (s *Service) waitSettled(ctx context.Context, max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if st := s.Status().State; st != Connecting && st != Disconnecting {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// proxyURL is the active core's SOCKS inbound as a proxy for the
// service's own requests, credentials included: never print it.
func (s *Service) proxyURL() *url.URL { return s.socks.ProxyURL(s.cfg.Listen) }

// newTransport is an HTTP transport through proxy (nil for none) whose
// connections give up when the other side goes quiet; the caller's
// context or client timeout bounds the whole request.
func newTransport(proxy *url.URL) *http.Transport {
	tr := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if proxy != nil {
		tr.Proxy = http.ProxyURL(proxy)
	}
	return tr
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
