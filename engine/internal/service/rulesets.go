package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"coreshift/engine/internal/fsutil"
	"coreshift/engine/internal/ruleset"
	"coreshift/engine/internal/tunlayer"
)

// geoSet is a rule set behind a routing preset. CoreShift carries a copy
// of each and downloads newer ones from ruleset.URL(Tag), see package
// ruleset.
type geoSet struct {
	Tag string
	// IP marks address-based sets (geoip), which the TUN layer matches after
	// resolving names.
	IP bool
	// Proxy marks sets that go through the tunnel even when a direct list
	// also has them.
	Proxy bool
	// Lost is what goes amiss without the set, for the journal.
	Lost string
}

// russiaSuffixes and russiaSets make up the "Russian sites direct" preset:
// the national domains, Russian services on other domains (geosite) and
// servers located in Russia (geoip). Sites blocked in Russia stay in the
// tunnel, many of them are on .ru (novayagazeta.ru, tvrain.ru): direct,
// they would not open. Blocked media come from SagerNet, every blocked
// site from runetfreedom (ru-blocked). With RussiaAbroad the preset's
// names go direct only where their servers are in Russia (geoip-ru).
// russiaAlways go direct wherever they are: 2ip.io is where 2ip.ru
// redirects, the usual way to check the preset works, which would
// otherwise show the tunnel's address.
var (
	russiaSuffixes = []string{"ru", "su", "xn--p1ai"}
	russiaAlways   = []string{"2ip.io"}
	russiaSets     = []geoSet{
		{Tag: "geosite-category-ru", Lost: "её сайты пойдут через туннель"},
		{Tag: "geoip-ru", IP: true, Lost: "российские адреса пойдут через туннель"},
		{Tag: "geosite-category-media-ru-blocked", Proxy: true, Lost: "заблокированные СМИ на .ru могут не открыться"},
		{Tag: "geosite-ru-blocked", Proxy: true, Lost: "заблокированные сайты на .ru могут не открыться"},
		googleSet,
	}
)

// adsSet is refused with BlockAds.
var adsSet = geoSet{Tag: "geosite-category-ads-all", Lost: "реклама не блокируется"}

// Google, YouTube included, stays in the tunnel with the Russian preset on,
// whatever address a name resolves to: geosite-google, and googleSuffixes
// for when that set is not downloaded yet or cannot be. A video or update
// server of Google Global Cache inside a Russian provider has that
// provider's address, which geoip-ru would send direct, where YouTube is
// slowed down; its links would also be refused there, being signed for the
// address that asked for them, the tunnel's. A connection made by such an
// address (a name the browser looked up before connecting, its own DNS
// over HTTPS, QUIC kept from before) matches by the name the TUN layer
// sniffs from it, which these rules see before geoip-ru; Google's own
// ranges are not in geoip-ru, so no address list is needed. google.ru is
// listed for the same reason: the preset sends .ru direct. Nothing the user
// sends direct takes Google out of the tunnel (they are pinned, see
// tunlayer.DNSOptions.PinnedSuffixes); only blocks come first, so with
// BlockAds Google's ad and analytics servers are refused.
var googleSet = geoSet{Tag: "geosite-google", Proxy: true, Lost: "Google пойдёт через туннель по встроенному списку"}

var googleSuffixes = []string{
	// Search and the national domains people in and around Russia meet.
	"google.com", "google.ru", "google.by", "google.kz", "google.com.ua", "google.am", "google.az", "google.ge",
	"google.co.uk", "google.de", "google.fr", "google.pl", "google.nl", "google.it", "google.es", "google.fi",
	"google.cz", "google.lv", "google.lt", "google.ee", "google.com.tr", "google.co.il", "g.co", "goo.gl",
	// Infrastructure: APIs, static files, user content, downloads and
	// updates (gvt*), the caches' reverse names.
	"googleapis.com", "gstatic.com", "googleusercontent.com", "gvt1.com", "gvt2.com", "gvt3.com", "1e100.net",
	"googledomains.com", "withgoogle.com", "recaptcha.net",
	// Ads and analytics, which sites embed.
	"googlesyndication.com", "googleadservices.com", "doubleclick.net", "google-analytics.com", "googletagmanager.com",
	"googletagservices.com", "app-measurement.com",
	// Android, Gmail, Chrome, Firebase.
	"android.com", "gmail.com", "googlemail.com", "chrome.com", "chromium.org", "firebaseio.com", "crashlytics.com",
	// YouTube.
	"youtube.com", "youtu.be", "yt.be", "youtube-nocookie.com", "youtubekids.com", "googlevideo.com", "ytimg.com", "ggpht.com",
}

const (
	ruleSetMaxAge  = 7 * 24 * time.Hour
	ruleSetTimeout = 20 * time.Second
	// setsRetry: a download that failed is not tried again sooner, so
	// reconnecting (a network change) does not wait for it each time.
	setsRetry = 10 * time.Minute
	// jobTimeout bounds a download in the background: a v2ray list of every
	// category can be tens of megabytes.
	jobTimeout = 3 * time.Minute
)

// setsWait is how long connecting waits for sets that are not on disk yet,
// all of them together; they keep downloading after it, for the next
// connection. A variable for tests.
var setsWait = 20 * time.Second

var (
	// errPending is a set still downloading when connecting stops waiting.
	errPending = errors.New("still downloading")
	// errNotBuiltIn is a set CoreShift carries but this build has not.
	errNotBuiltIn = errors.New("not built into this build of CoreShift")
)

// ruleSets keeps rule set files in dir: the copies built into CoreShift at
// first, then newer ones from SagerNet, refreshed in the background when
// old. A downloaded copy replaces the one on disk only if ruleset.Check
// accepts it next to that one and the built-in one. The sets of the user's
// rules, from the source the user chose, are kept below dir/geo (see
// georules.go).
type ruleSets struct {
	dir   string
	fetch func(ctx context.Context, url string, proxy *url.URL) ([]byte, error)
	// fetchGeo downloads from the sources the user chose, at most limit
	// bytes; nil means httpsGet.
	fetchGeo func(ctx context.Context, url string, proxy *url.URL, limit int64) ([]byte, error)
	// transport is how httpsGet connects through proxy (nil for none);
	// nil means newTransport.
	transport func(proxy *url.URL) *http.Transport
	// baseline returns the built-in copy of a set and when it was
	// downloaded: ruleset.Baseline.
	baseline func(tag string) ([]byte, time.Time, bool)
	publish  func(Event)
	// late is called when a set connecting stopped waiting for has come:
	// the next connection has it.
	late func()

	mu         sync.Mutex
	refreshing map[string]bool
	jobs       map[string]*job    // downloads under way, by the file they make
	failed     map[string]failure // recent failed downloads, by file
	missed     map[string]bool    // files the running connection is without
	extract    sync.Mutex         // one category out of a v2ray list at a time
}

func newRuleSets(dir string, publish func(Event)) *ruleSets {
	return &ruleSets{dir: dir, fetch: fetchRuleSet, baseline: ruleset.Baseline, publish: publish, refreshing: map[string]bool{}}
}

// job is a download in the background.
type job struct {
	done chan struct{}
	err  error
}

type failure struct {
	at  time.Time
	err error
}

// start runs do in the background to make path, unless it runs already.
func (r *ruleSets) start(path string, do func(ctx context.Context) error) *job {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.jobs == nil {
		r.jobs, r.failed, r.missed = map[string]*job{}, map[string]failure{}, map[string]bool{}
	}
	if j := r.jobs[path]; j != nil {
		return j
	}
	j := &job{done: make(chan struct{})}
	r.jobs[path] = j
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		err := do(ctx)
		cancel()
		r.mu.Lock()
		j.err = err
		delete(r.jobs, path)
		late := false
		if err != nil {
			r.failed[path] = failure{time.Now(), err}
		} else {
			delete(r.failed, path)
			late = r.missed[path]
			delete(r.missed, path)
		}
		r.mu.Unlock()
		close(j.done)
		if late && r.late != nil {
			r.late()
		}
	}()
	return j
}

// await makes path with do, as start does, and waits for it while ctx
// lasts: errPending then. A download that failed less than setsRetry ago
// is not tried again; its error is returned at once.
func (r *ruleSets) await(ctx context.Context, path string, do func(ctx context.Context) error) error {
	r.mu.Lock()
	f, failed := r.failed[path]
	r.mu.Unlock()
	if failed && time.Since(f.at) < setsRetry {
		return f.err
	}
	j := r.start(path, do)
	select {
	case <-j.done:
		return j.err
	case <-ctx.Done():
		r.mu.Lock()
		r.missed[path] = true
		r.mu.Unlock()
		return errPending
	}
}

// get returns the sets available on disk, split into direct domain, direct
// IP and proxy sets, publishing what became of each (see builtin).
func (r *ruleSets) get(ctx context.Context, sets []geoSet, proxy *url.URL) (domain, ip, proxied []tunlayer.RuleSet) {
	for _, gs := range sets {
		path, err := r.builtin(ctx, gs, proxy)
		if err != nil {
			r.lost(gs, err)
			continue
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

// builtin returns the file of the preset set gs. A set missing from disk,
// damaged there or older than the built-in copy is replaced by the
// built-in copy; one CoreShift has no copy of is downloaded now, through
// proxy (the active core's SOCKS inbound, credentials included; nil for
// none) and then directly, while ctx lasts. One that cannot be had is left
// out by the caller, so connecting still works, only without what the set
// does.
func (r *ruleSets) builtin(ctx context.Context, gs geoSet, proxy *url.URL) (string, error) {
	path := filepath.Join(r.dir, gs.Tag+".srs")
	mod, ok := r.ready(gs, path)
	_, _, fromDat := ruleset.DatSource(gs.Tag)
	switch {
	case fromDat && !ok:
		// Made out of a list of every category, tens of megabytes:
		// not downloaded by each device, only with releases.
		return "", errNotBuiltIn
	case fromDat:
	case !ok:
		err := r.await(ctx, path, func(ctx context.Context) error {
			if err := r.download(ctx, gs, path, proxy); err != nil {
				return err
			}
			r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "downloaded"})
			return nil
		})
		if err != nil {
			return "", err
		}
	case time.Since(mod) > ruleSetMaxAge:
		go r.refresh(gs, path, proxy)
	}
	return path, nil
}

// lost reports a preset set that could not be had.
func (r *ruleSets) lost(gs geoSet, err error) {
	what := gs.Lost
	if what == "" {
		what = "её сайты пойдут через туннель"
	}
	if errors.Is(err, errPending) {
		r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "pending",
			Error: fmt.Sprintf("база %s ещё загружается, до переподключения %s", gs.Tag, what)})
		return
	}
	r.publish(Event{Kind: "rules", Reason: gs.Tag, Error: fmt.Sprintf("база %s не загрузилась, %s: %v", gs.Tag, what, err)})
}

// refresh replaces an old file in the background; the TUN layer picks the
// new one up on its next start.
func (r *ruleSets) refresh(gs geoSet, path string, proxy *url.URL) {
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
		// Line "kept": the set still works, only not updated.
		r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "kept", Error: fmt.Sprintf("база %s не обновилась, работает прежняя: %v", gs.Tag, err)})
		return
	}
	r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "updated"})
}

// ready makes sure path holds a usable copy of gs and returns when it was
// downloaded. A link or anything else in place of a file the service wrote,
// a damaged file, or one older than the built-in copy gives way to the
// built-in copy, dated as downloaded. false means there is neither.
func (r *ruleSets) ready(gs geoSet, path string) (time.Time, bool) {
	var cur []byte
	var mod time.Time
	fi, err := os.Lstat(path)
	switch {
	case err != nil:
	case !fi.Mode().IsRegular():
		os.RemoveAll(path)
	default:
		cur, err = os.ReadFile(path)
		if err == nil {
			err = ruleset.Check(gs.Tag, cur)
		}
		if err != nil {
			r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "damaged", Error: fmt.Sprintf("база %s испорчена: %v", gs.Tag, err)})
			os.Remove(path)
			cur = nil
		}
		mod = fi.ModTime()
	}
	base, fetched, haveBase := r.baseline(gs.Tag)
	if cur != nil && (!haveBase || !mod.Before(fetched)) {
		return mod, true
	}
	if !haveBase {
		return time.Time{}, false
	}
	if err := writeAtomic(path, base); err != nil {
		r.publish(Event{Kind: "rules", Reason: gs.Tag, Error: fmt.Sprintf("база %s не записалась: %v", gs.Tag, err)})
		return time.Time{}, false
	}
	os.Chtimes(path, fetched, fetched)
	r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "builtin"})
	return fetched, true
}

// download fetches gs into path, if ruleset.Check accepts it next to the
// copy on disk and the built-in one.
func (r *ruleSets) download(ctx context.Context, gs geoSet, path string, proxy *url.URL) error {
	var refs [][]byte
	if cur, err := os.ReadFile(path); err == nil {
		refs = append(refs, cur)
	}
	if base, _, ok := r.baseline(gs.Tag); ok {
		refs = append(refs, base)
	}
	// Through the proxy first: the source may be blocked where the user is.
	vias := []*url.URL{nil}
	if proxy != nil {
		vias = []*url.URL{proxy, nil}
	}
	var errs []error
	for _, via := range vias {
		b, err := r.fetch(ctx, ruleset.URL(gs.Tag), via)
		if err == nil {
			err = ruleset.Check(gs.Tag, b, refs...)
		}
		if err == nil {
			return writeAtomic(path, b)
		}
		how := "directly"
		if via != nil {
			how = "through the proxy"
		}
		errs = append(errs, fmt.Errorf("%s: %w", how, err))
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(errs...)
}

func fetchRuleSet(ctx context.Context, rawURL string, proxy *url.URL) ([]byte, error) {
	tr := newTransport(proxy)
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
	b, err := io.ReadAll(io.LimitReader(resp.Body, ruleset.MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > ruleset.MaxSize {
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
