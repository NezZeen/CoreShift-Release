package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/ruleset"
	"coreshift/engine/internal/store"
	"coreshift/engine/internal/tunlayer"
)

// The user's own rules (store.Routing.Rules) and where their categories
// come from (store.GeoSource): SagerNet, runetfreedom or the user's own
// links, each a URL with {name} (a rule set per category) or a v2ray list
// (geosite.dat, geoip.dat) the categories are taken out of. A set from
// there is only checked to be a rule set of names, or of addresses, within
// the size limits: what is in it is the source's, the user chose it. Sets
// are kept under rules/geo, a folder per link, and refreshed weekly in the
// background, as the built-in ones are.

// geoPrune: a folder of a source no longer used goes after this long.
const geoPrune = 30 * 24 * time.Hour

// geoSource is where categories come from: a link for names, one for
// addresses.
type geoSource struct{ site, ip string }

func sourceOf(g store.GeoSource) geoSource {
	switch g.Source {
	case store.GeoRunetFreedom:
		// Names only: its list has no addresses.
		return geoSource{ruleset.RunetFreedomDat, ruleset.SagerNetGeoIP}
	case store.GeoCustom:
		src := geoSource{g.GeositeURL, g.GeoIPURL}
		if src.site == "" {
			src.site = ruleset.SagerNetGeosite
		}
		if src.ip == "" {
			src.ip = ruleset.SagerNetGeoIP
		}
		return src
	}
	return geoSource{ruleset.SagerNetGeosite, ruleset.SagerNetGeoIP}
}

func (g geoSource) of(ip bool) string {
	if ip {
		return g.ip
	}
	return g.site
}

func isSagerNet(link string) bool {
	return link == ruleset.SagerNetGeosite || link == ruleset.SagerNetGeoIP
}

// sourceName names where link leads, for the journal: never the link,
// which may carry a token.
func sourceName(link string) msg.Msg {
	switch link {
	case ruleset.SagerNetGeosite, ruleset.SagerNetGeoIP:
		return msg.Raw("SagerNet")
	case ruleset.RunetFreedomDat:
		return msg.Raw("runetfreedom")
	}
	return msg.New("rules.source.own")
}

func kindOf(ip bool) string {
	if ip {
		return "geoip"
	}
	return "geosite"
}

// linkKey names the folder of a link's files.
func linkKey(link string) string {
	sum := sha256.Sum256([]byte(link))
	return hex.EncodeToString(sum[:8])
}

// geoRouting is what the rule sets give a connection: the presets' sets
// (block is the ad set) and the user's rules.
type geoRouting struct {
	block, pinned, proxied, domain, ip []tunlayer.RuleSet
	rules                              []tunlayer.Rule
}

// routing gets the sets of o: the presets' (from the chosen source with
// Geo.Presets, else the built-in ones), those of the user's rules, and the
// ad set. It waits at most setsWait for those not on disk yet, through
// proxy (the active core) and then directly; one it cannot have is
// reported, and what needs it left out: connecting never fails for a set.
func (r *ruleSets) routing(ctx context.Context, o Options, proxy *url.URL) geoRouting {
	ctx, cancel := context.WithTimeout(ctx, setsWait)
	defer cancel()
	r.mu.Lock()
	r.missed = map[string]bool{}
	r.mu.Unlock()
	src := sourceOf(o.Geo)
	fromSource := o.Geo.Presets && o.Geo.Source != store.GeoSagerNet

	var presets []geoSet
	if o.DNS.RussiaDirect && !o.Selective {
		presets = slices.Clone(russiaSets)
	}
	if o.BlockAds {
		presets = append(presets, adsSet)
	}
	type found struct {
		tag, path string
		err       error
	}
	got := make([]found, len(presets))
	user := make([]found, len(o.Rules))
	var wg sync.WaitGroup
	for i, gs := range presets {
		wg.Go(func() {
			p, err := r.preset(ctx, gs, src, fromSource, proxy)
			got[i] = found{gs.Tag, p, err}
		})
	}
	for i, rule := range o.Rules {
		kind, name := rule.Parse()
		if kind != store.MatchGeosite && kind != store.MatchGeoIP {
			continue
		}
		wg.Go(func() {
			tag, p, err := r.userSet(ctx, src, kind == store.MatchGeoIP, name, proxy)
			user[i] = found{tag, p, err}
		})
	}
	wg.Wait()

	var out geoRouting
	for i, gs := range presets {
		if got[i].err != nil {
			r.lost(gs, got[i].err)
			continue
		}
		rs := tunlayer.RuleSet{Tag: gs.Tag, Path: got[i].path}
		switch {
		case gs.Tag == adsSet.Tag:
			out.block = append(out.block, rs)
		case gs.Tag == googleSet.Tag:
			out.pinned = append(out.pinned, rs)
		case gs.Proxy:
			out.proxied = append(out.proxied, rs)
		case gs.IP:
			out.ip = append(out.ip, rs)
		default:
			out.domain = append(out.domain, rs)
		}
	}
	for i, rule := range o.Rules {
		tr, ok := plainRule(rule)
		if kind, _ := rule.Parse(); kind == store.MatchGeosite || kind == store.MatchGeoIP {
			ip := kind == store.MatchGeoIP
			if user[i].err != nil {
				r.skipped(rule, src.of(ip), user[i].err)
				continue
			}
			tr.Set, tr.SetIP, ok = &tunlayer.RuleSet{Tag: user[i].tag, Path: user[i].path}, ip, true
		}
		if ok {
			out.rules = append(out.rules, tr)
		}
	}
	r.prune(src)
	return out
}

// plainRule returns rule for the TUN layer, if it is of a name or an
// address; one of a category needs its set.
func plainRule(rule store.Rule) (tunlayer.Rule, bool) {
	tr := tunlayer.Rule{Action: rule.Action}
	switch kind, v := rule.Parse(); kind {
	case store.MatchDomain:
		tr.Domains = []string{v}
	case store.MatchIP:
		p, err := netip.ParsePrefix(v)
		if err != nil {
			a, err := netip.ParseAddr(v)
			if err != nil {
				return tr, false // the store takes only addresses and subnets
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		tr.IPs = []netip.Prefix{p}
	default:
		return tr, false
	}
	return tr, true
}

// preset returns the file of the preset set gs: from src with fromSource,
// unless src is SagerNet's (whose sets CoreShift carries) or the set cannot
// be had from it; the built-in one then.
func (r *ruleSets) preset(ctx context.Context, gs geoSet, src geoSource, fromSource bool, proxy *url.URL) (string, error) {
	if link := src.of(gs.IP); fromSource && !isSagerNet(link) {
		name := strings.TrimPrefix(strings.TrimPrefix(gs.Tag, "geosite-"), "geoip-")
		p, err := r.category(ctx, link, gs.IP, name, proxy)
		if err == nil {
			return p, nil
		}
		set := kindOf(gs.IP) + ":" + name
		m := msg.New("rules.fallback.failed", "set", set, "src", sourceName(link), "err", msg.Raw(err.Error()), "tag", gs.Tag)
		if errors.Is(err, errPending) {
			m = msg.New("rules.fallback.pending", "set", set, "src", sourceName(link), "tag", gs.Tag)
		}
		r.publish(Event{Kind: "rules", Reason: gs.Tag, Line: "fallback"}.withError(m))
	}
	return r.builtin(ctx, gs, proxy)
}

// userSet returns the tag and file of a category of the user's rules. One
// of SagerNet that CoreShift carries is the built-in set, held to its
// checks.
func (r *ruleSets) userSet(ctx context.Context, src geoSource, ip bool, name string, proxy *url.URL) (tag, path string, err error) {
	link := src.of(ip)
	tag = kindOf(ip) + "-" + name
	if isSagerNet(link) && slices.Contains(ruleset.Known(), tag) {
		path, err = r.builtin(ctx, geoSet{Tag: tag, IP: ip}, proxy)
		return tag, path, err
	}
	path, err = r.category(ctx, link, ip, name, proxy)
	return "user-" + tag, path, err
}

// skipped reports a rule left out for want of its set.
func (r *ruleSets) skipped(rule store.Rule, link string, err error) {
	action := msg.Raw("")
	if a, ok := map[string]string{store.RuleProxy: "proxy", store.RuleDirect: "direct", store.RuleBlock: "block"}[rule.Action]; ok {
		action = msg.New("rules.action." + a)
	}
	m := msg.New("rules.skipped.failed", "match", rule.Match, "action", action, "src", sourceName(link), "err", msg.Raw(err.Error()))
	if errors.Is(err, errPending) {
		m = msg.New("rules.skipped.pending", "match", rule.Match, "action", action, "src", sourceName(link))
	}
	r.publish(Event{Kind: "rules", Reason: rule.Match, Line: "skipped"}.withError(m))
}

// category returns the rule set file of the category name (of addresses
// with ip) from link: downloaded from the link with {name} put in, or
// taken out of the v2ray list the link leads to. One not on disk yet is
// waited for while ctx lasts.
func (r *ruleSets) category(ctx context.Context, link string, ip bool, name string, proxy *url.URL) (string, error) {
	kind := kindOf(ip)
	dir := filepath.Join(r.dir, "geo", linkKey(link))
	out := filepath.Join(dir, kind+"-"+name+".srs")
	label := kind + ":" + name
	if strings.Contains(link, "{name}") {
		get := func(ctx context.Context) error {
			b, err := r.viaProxy(ctx, proxy, strings.Replace(link, "{name}", url.PathEscape(name), 1), ruleset.MaxSize)
			if err == nil {
				err = ruleset.Validate(b, ip)
			}
			if err != nil {
				return err
			}
			return writeAtomic(out, b)
		}
		if mod, ok := usable(out, ip); ok {
			if time.Since(mod) > ruleSetMaxAge {
				r.refreshGeo(out, label, get)
			}
			return out, nil
		}
		return out, r.await(ctx, out, announce(r, label, get))
	}

	list := filepath.Join(dir, kind+".dat")
	get := func(ctx context.Context) error {
		b, err := r.viaProxy(ctx, proxy, link, ruleset.MaxDatSize)
		if err == nil {
			err = ruleset.CheckDat(b)
		}
		if err != nil {
			return err
		}
		return writeAtomic(list, b)
	}
	fi, err := os.Lstat(list)
	if err == nil && !fi.Mode().IsRegular() {
		os.RemoveAll(list)
		err = os.ErrNotExist
	}
	if err != nil {
		if err := r.await(ctx, list, announce(r, kind+".dat", get)); err != nil {
			return "", err
		}
		if fi, err = os.Stat(list); err != nil {
			return "", err
		}
	} else if time.Since(fi.ModTime()) > ruleSetMaxAge {
		r.refreshGeo(list, kind+".dat", get)
	}
	if mod, ok := usable(out, ip); ok && !mod.Before(fi.ModTime()) {
		return out, nil
	}
	// One at a time: a list can be tens of megabytes.
	r.extract.Lock()
	defer r.extract.Unlock()
	dat, err := os.ReadFile(list)
	if err != nil {
		return "", err
	}
	b, err := ruleset.FromDat(dat, ip, name)
	if err != nil {
		return "", err
	}
	return out, writeAtomic(out, b)
}

// usable reports whether path holds a rule set of addresses (ip) or of
// names, and when it was written. Anything else there is removed.
func usable(path string, ip bool) (time.Time, bool) {
	fi, err := os.Lstat(path)
	if err != nil {
		return time.Time{}, false
	}
	if fi.Mode().IsRegular() {
		if b, err := os.ReadFile(path); err == nil && ruleset.Validate(b, ip) == nil {
			return fi.ModTime(), true
		}
	}
	os.RemoveAll(path)
	return time.Time{}, false
}

// announce journals a set downloaded by do.
func announce(r *ruleSets, label string, do func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := do(ctx); err != nil {
			return err
		}
		r.publish(Event{Kind: "rules", Reason: label, Line: "downloaded"})
		return nil
	}
}

// refreshGeo replaces an old file with do in the background.
func (r *ruleSets) refreshGeo(path, label string, do func(context.Context) error) {
	r.start(path, func(ctx context.Context) error {
		if err := do(ctx); err != nil {
			r.publish(Event{Kind: "rules", Reason: label, Line: "kept"}.withError(msg.New("rules.kept", "set", label, "err", msg.Raw(err.Error()))))
			return err
		}
		r.publish(Event{Kind: "rules", Reason: label, Line: "updated"})
		return nil
	})
}

// prune removes the folders of sources not used for geoPrune.
func (r *ruleSets) prune(src geoSource) {
	entries, err := os.ReadDir(filepath.Join(r.dir, "geo"))
	if err != nil {
		return
	}
	keep := []string{linkKey(src.site), linkKey(src.ip)}
	for _, e := range entries {
		if slices.Contains(keep, e.Name()) {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > geoPrune {
			os.RemoveAll(filepath.Join(r.dir, "geo", e.Name()))
		}
	}
}

// viaProxy downloads link through proxy first, then directly: the source
// may be blocked where the user is.
func (r *ruleSets) viaProxy(ctx context.Context, proxy *url.URL, link string, limit int64) ([]byte, error) {
	get := r.fetchGeo
	if get == nil {
		get = r.httpsGet
	}
	vias := []*url.URL{nil}
	if proxy != nil {
		vias = []*url.URL{proxy, nil}
	}
	var errs []error
	for _, via := range vias {
		b, err := get(ctx, link, via, limit)
		if err == nil {
			return b, nil
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
	return nil, errors.Join(errs...)
}

// httpsGet downloads link, at most limit bytes, over https only, redirects
// included.
func (r *ruleSets) httpsGet(ctx context.Context, link string, proxy *url.URL, limit int64) ([]byte, error) {
	if u, err := url.Parse(link); err != nil || u.Scheme != "https" {
		return nil, errors.New("only https links are taken")
	}
	newTr := r.transport
	if newTr == nil {
		newTr = newTransport
	}
	tr := newTr(proxy)
	defer tr.CloseIdleConnections()
	if limit <= ruleset.MaxSize {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ruleSetTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, errors.New("invalid link")
	}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("redirected to a link without https")
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		// Without the link, which may carry a token.
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
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("larger than %d MB", limit>>20)
	}
	return b, nil
}
