package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"coreshift/engine/internal/fsutil"
	"coreshift/engine/internal/tunlayer"
)

// geoSet is a rule set behind a routing preset.
type geoSet struct {
	Tag string
	URL string
	// IP marks address-based sets (geoip), which the TUN layer matches after
	// resolving names.
	IP bool
	// Proxy marks sets that go through the tunnel even when a direct list
	// also has them.
	Proxy bool
}

// russiaSuffixes and russiaSets make up the "Russian sites direct" preset:
// the national domains, Russian services on other domains (geosite) and
// servers located in Russia (geoip). Media blocked in Russia stay in the
// tunnel, many of them are on .ru (novayagazeta.ru, tvrain.ru): direct,
// they would not open. 2ip.io is where 2ip.ru redirects: the usual way to
// check the preset works would otherwise show the tunnel's address.
var (
	russiaSuffixes = []string{"ru", "su", "xn--p1ai", "2ip.io"}
	russiaSets     = []geoSet{
		{Tag: "geosite-category-ru", URL: "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-category-ru.srs"},
		{Tag: "geoip-ru", URL: "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-ru.srs", IP: true},
		{Tag: "geosite-category-media-ru-blocked",
			URL:   "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-category-media-ru-blocked.srs",
			Proxy: true},
	}
)

const (
	ruleSetMaxAge  = 7 * 24 * time.Hour
	ruleSetTimeout = 20 * time.Second
	ruleSetMaxSize = 16 << 20
)

// ruleSets keeps rule set files in dir, downloading them when missing and
// refreshing them in the background when old.
type ruleSets struct {
	dir     string
	fetch   func(ctx context.Context, url string, proxy netip.AddrPort) ([]byte, error)
	publish func(Event)

	mu         sync.Mutex
	refreshing map[string]bool
}

func newRuleSets(dir string, publish func(Event)) *ruleSets {
	return &ruleSets{dir: dir, fetch: fetchRuleSet, publish: publish, refreshing: map[string]bool{}}
}

// get returns the sets available on disk, split into direct domain, direct
// IP and proxy sets. A
// missing set is downloaded now, through proxy (the active core) and then
// directly; one that cannot be had is reported and left out, so connecting
// still works, only with fewer names going direct.
func (r *ruleSets) get(ctx context.Context, sets []geoSet, proxy netip.AddrPort) (domain, ip, proxied []tunlayer.RuleSet) {
	for _, gs := range sets {
		path := filepath.Join(r.dir, gs.Tag+".srs")
		fi, err := os.Lstat(path)
		if err == nil && !fi.Mode().IsRegular() {
			// A link or anything else in place of a file the service
			// wrote: fetched anew.
			os.RemoveAll(path)
			err = fs.ErrNotExist
		}
		switch {
		case err != nil:
			if err := r.download(ctx, gs, path, proxy); err != nil {
				r.publish(Event{Kind: "rules", Reason: gs.Tag,
					Error: fmt.Sprintf("rule set %s unavailable, its sites go through the tunnel: %v", gs.Tag, err)})
				continue
			}
			r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "downloaded"})
		case time.Since(fi.ModTime()) > ruleSetMaxAge:
			go r.refresh(gs, path, proxy)
		}
		rs := tunlayer.RuleSet{Tag: gs.Tag, Path: path}
		switch {
		case gs.Proxy:
			proxied = append(proxied, rs)
		case gs.IP:
			ip = append(ip, rs)
		default:
			domain = append(domain, rs)
		}
	}
	return domain, ip, proxied
}

// refresh replaces an old file in the background; the TUN layer picks the
// new one up on its next start.
func (r *ruleSets) refresh(gs geoSet, path string, proxy netip.AddrPort) {
	r.mu.Lock()
	if r.refreshing[gs.Tag] {
		r.mu.Unlock()
		return
	}
	r.refreshing[gs.Tag] = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.refreshing, gs.Tag)
		r.mu.Unlock()
	}()
	if err := r.download(context.Background(), gs, path, proxy); err != nil {
		r.publish(Event{Kind: "rules", Reason: gs.Tag, Error: fmt.Sprintf("refresh rule set %s (keeping the old one): %v", gs.Tag, err)})
		return
	}
	r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "updated"})
}

func (r *ruleSets) download(ctx context.Context, gs geoSet, path string, proxy netip.AddrPort) error {
	// Through the proxy first: the source may be blocked where the user is.
	vias := []netip.AddrPort{{}}
	if proxy.IsValid() {
		vias = []netip.AddrPort{proxy, {}}
	}
	var errs []error
	for _, via := range vias {
		b, err := r.fetch(ctx, gs.URL, via)
		if err == nil {
			err = checkRuleSet(b)
		}
		if err == nil {
			return writeAtomic(path, b)
		}
		how := "directly"
		if via.IsValid() {
			how = "through the proxy"
		}
		errs = append(errs, fmt.Errorf("%s: %w", how, err))
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(errs...)
}

// checkRuleSet rejects what is not a binary rule set, such as an error page.
func checkRuleSet(b []byte) error {
	if !bytes.HasPrefix(b, []byte("SRS")) {
		return errors.New("response is not a rule set")
	}
	return nil
}

func fetchRuleSet(ctx context.Context, rawURL string, proxy netip.AddrPort) ([]byte, error) {
	tr := &http.Transport{}
	if proxy.IsValid() {
		tr.Proxy = http.ProxyURL(&url.URL{Scheme: "socks5", Host: proxy.String()})
	}
	defer tr.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, ruleSetTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, ruleSetMaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > ruleSetMaxSize {
		return nil, errors.New("rule set too large")
	}
	return b, nil
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, b, 0o600)
}
