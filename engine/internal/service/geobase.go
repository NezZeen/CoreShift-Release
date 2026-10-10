package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"coreshift/engine/internal/ruleset"
	"coreshift/engine/internal/store"
)

// The base of a source is the file its categories are listed in: the
// v2ray list itself (geosite.dat, geoip.dat), or, for SagerNet, whose
// sources are one file per category, the listing of its branch (taken from
// GitHub, weekly). Any other link with {name} lists nothing: its categories
// are only tried. The names typed in the user's rules are matched against
// the base (ruleset.Resolve), so a category that is new in a base works at
// once, and a wrong name is told from a base that did not download.

// baseKind is what a link's base is.
type baseKind int

const (
	baseNone  baseKind = iota // the link lists nothing
	baseDat                   // a v2ray list
	baseIndex                 // the listing of SagerNet's branch
)

// base returns the file the base of link is kept in.
func (r *ruleSets) base(link string, ip bool) (string, baseKind) {
	dir := filepath.Join(r.dir, "geo", linkKey(link))
	if !strings.Contains(link, "{name}") {
		return filepath.Join(dir, kindOf(ip)+".dat"), baseDat
	}
	if _, ok := ruleset.IndexOf(link); ok {
		return filepath.Join(dir, kindOf(ip)+".index"), baseIndex
	}
	return "", baseNone
}

// baseLabel names the base for the journal.
func baseLabel(ip bool, k baseKind) string {
	if k == baseIndex {
		return kindOf(ip) + "-index"
	}
	return kindOf(ip) + ".dat"
}

// progress is how much of a download is there.
type progress struct{ got, total atomic.Int64 }

type progressKey struct{}

// track makes ctx carry the progress of the download that makes path.
func (r *ruleSets) track(ctx context.Context, path string) context.Context {
	p := &progress{}
	r.mu.Lock()
	r.lazy()
	r.prog[path] = p
	r.mu.Unlock()
	return context.WithValue(ctx, progressKey{}, p)
}

func (r *ruleSets) untrack(path string) {
	r.mu.Lock()
	delete(r.prog, path)
	r.mu.Unlock()
}

// countReader counts what is read into p.
type countReader struct {
	r io.Reader
	p *progress
}

func (c *countReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.p.got.Add(int64(n))
	return n, err
}

// getter returns the function that downloads the base of link into path.
func (r *ruleSets) getter(link string, ip bool, path string, k baseKind, proxy *url.URL) func(context.Context) error {
	switch k {
	case baseDat:
		return func(ctx context.Context) error {
			ctx = r.track(ctx, path)
			defer r.untrack(path)
			b, err := r.viaProxy(ctx, proxy, link, ruleset.MaxDatSize)
			if err == nil {
				err = ruleset.CheckDat(b)
			}
			if err != nil {
				return err
			}
			return writeAtomic(path, b)
		}
	case baseIndex:
		index, _ := ruleset.IndexOf(link)
		return func(ctx context.Context) error {
			ctx = r.track(ctx, path)
			defer r.untrack(path)
			b, err := r.viaProxy(ctx, proxy, index, ruleset.MaxIndexSize)
			var names []string
			if err == nil {
				names, err = ruleset.ParseIndex(b, kindOf(ip))
			}
			if err != nil {
				return err
			}
			j, err := json.Marshal(names)
			if err != nil {
				return err
			}
			return writeAtomic(path, j)
		}
	}
	return nil
}

// ensureBase makes sure the base of link is on disk, waiting for it while
// ctx lasts (errPending then); an old one is renewed in the background.
func (r *ruleSets) ensureBase(ctx context.Context, link string, ip bool, proxy *url.URL) (string, baseKind, error) {
	path, k := r.base(link, ip)
	if k == baseNone {
		return "", k, nil
	}
	get := r.getter(link, ip, path, k, proxy)
	label := baseLabel(ip, k)
	fi, err := os.Lstat(path)
	if err == nil && !fi.Mode().IsRegular() {
		os.RemoveAll(path)
		err = os.ErrNotExist
	}
	if err != nil {
		return path, k, r.await(ctx, path, announce(r, label, get))
	}
	if time.Since(fi.ModTime()) > ruleSetMaxAge {
		r.refreshGeo(path, label, get)
	}
	return path, k, nil
}

// prefetch starts downloading the base of link in the background, unless
// it is on disk, is on its way or failed a moment ago; force does not mind
// the failure and the copy on disk.
func (r *ruleSets) prefetch(link string, ip bool, proxy *url.URL, force bool) {
	path, k := r.base(link, ip)
	if k == baseNone {
		return
	}
	r.mu.Lock()
	r.lazy()
	if force {
		delete(r.failed, path)
	}
	f, failed := r.failed[path]
	r.mu.Unlock()
	if failed && time.Since(f.at) < setsRetry {
		return
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() && !force {
		return
	}
	r.start(path, announce(r, baseLabel(ip, k), r.getter(link, ip, path, k, proxy)))
}

type catalog struct {
	mod   time.Time
	size  int64
	names []string
}

// namesOf returns the categories in the base file at path, and when it was
// written. The names are kept until the file changes: a list of tens of
// megabytes is not read for each key typed.
func (r *ruleSets) namesOf(path string, k baseKind) ([]string, time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	r.mu.Lock()
	r.lazy()
	c, ok := r.cats[path]
	r.mu.Unlock()
	if ok && c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
		return c.names, c.mod, nil
	}
	// One at a time: every rule asks at once, and a list can be tens of
	// megabytes.
	r.namesMu.Lock()
	defer r.namesMu.Unlock()
	r.mu.Lock()
	c, ok = r.cats[path]
	r.mu.Unlock()
	if ok && c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
		return c.names, c.mod, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var names []string
	switch k {
	case baseDat:
		names, err = ruleset.Categories(b)
	case baseIndex:
		err = json.Unmarshal(b, &names)
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	r.mu.Lock()
	r.cats[path] = catalog{fi.ModTime(), fi.Size(), names}
	r.mu.Unlock()
	return names, fi.ModTime(), nil
}

// listing returns the categories of the base of link, waiting for it while
// ctx lasts; listed is false for a link that lists nothing.
func (r *ruleSets) listing(ctx context.Context, link string, ip bool, proxy *url.URL) (names []string, listed bool, err error) {
	path, k, err := r.ensureBase(ctx, link, ip, proxy)
	if k == baseNone || err != nil {
		return nil, false, err
	}
	names, _, err = r.namesOf(path, k)
	if err != nil {
		os.Remove(path) // damaged: downloaded again next time
		return nil, false, err
	}
	return names, true, nil
}

// lazy makes the maps; r.mu is held.
func (r *ruleSets) lazy() {
	if r.jobs == nil {
		r.jobs, r.failed, r.missed = map[string]*job{}, map[string]failure{}, map[string]bool{}
	}
	if r.prog == nil {
		r.prog = map[string]*progress{}
	}
	if r.cats == nil {
		r.cats = map[string]catalog{}
	}
	if r.noted == nil {
		r.noted = map[string]bool{}
	}
}

// fileSafe is name as a part of a file's name: itself when it is a plain
// category, else a hash.
func fileSafe(name string) string {
	if ruleset.SafeName(name) {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return "x-" + hex.EncodeToString(sum[:6])
}

// An error to say what a rule's category came to.

// missingError: the base has no such category; the names near it, if the
// base is known, may be what was meant.
type missingError struct {
	name        string
	suggestions []string
	listed      bool
}

func (e *missingError) Error() string { return "no category " + `"` + e.name + `"` + " in the base" }

// ambiguousError: several categories are the name by spelling, none the
// name itself.
type ambiguousError struct {
	name       string
	candidates []string
}

func (e *ambiguousError) Error() string {
	return "name " + `"` + e.name + `"` + " fits several categories: " + strings.Join(e.candidates, ", ")
}

// httpStatusError is an answer other than 200.
type httpStatusError struct {
	code   int
	status string
}

func (e *httpStatusError) Error() string { return "server returned " + e.status }

// isNotFound reports whether any of the attempts behind err was answered
// "404 Not Found": the server is there, the file is not.
func isNotFound(err error) bool {
	var se *httpStatusError
	if errors.As(err, &se) && se.code == 404 {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "server returned 404")
}

// categoryOf resolves the name of a user's rule against the base of link:
// the category to use, or why there is none. names are nil, listed false,
// for a link that lists nothing, or whose listing cannot be had (SagerNet's
// sets are tried by the name as typed then).
func (r *ruleSets) categoryOf(ctx context.Context, link string, ip bool, name string, proxy *url.URL) (string, error) {
	tag := kindOf(ip) + "-" + name
	if isSagerNet(link) && slices.Contains(ruleset.Known(), tag) {
		return name, nil
	}
	names, listed, err := r.listing(ctx, link, ip, proxy)
	if err != nil {
		if _, k := r.base(link, ip); k == baseIndex {
			return name, nil // the sets are tried as typed
		}
		return "", err
	}
	if !listed {
		return name, nil
	}
	res := ruleset.Resolve(names, name)
	switch {
	case res.Name != "":
		return res.Name, nil
	case len(res.Ambiguous) > 0:
		return "", &ambiguousError{name, res.Ambiguous}
	}
	return "", &missingError{name: name, suggestions: res.Suggestions, listed: true}
}

// noteOnce reports whether key is new, and remembers it.
func (r *ruleSets) noteOnce(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lazy()
	if r.noted[key] {
		return false
	}
	r.noted[key] = true
	return true
}

// forGeo is the part of the settings the bases depend on.
func geoOf(set store.Settings) (geoSource, []store.Rule) {
	return sourceOf(set.Routing.Geo), set.Routing.Rules
}
