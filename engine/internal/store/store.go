// Package store keeps what the user set up: settings, subscriptions with
// their nodes, and the selected node. It persists everything to one JSON file
// and refreshes subscriptions in the background.
//
// The file holds subscription URLs (they carry access tokens) and node
// credentials, so it is written readable by the owner only; the caller keeps
// it in a directory closed to other users.
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"coreshift/engine/internal/fsutil"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/subscription"
)

var (
	ErrNotFound = errors.New("no such subscription")
	ErrExists   = errors.New("this subscription is already added")
	// ErrServersExist means every pasted server is in the list already.
	ErrServersExist = errors.New("these servers are already added")
	// ErrReset means the file could not be used and was moved aside;
	// Open still returns a working store with defaults.
	ErrReset = errors.New("store reset")
)

// FetchError is a failure to download a subscription, as opposed to a
// problem with what the user entered.
type FetchError struct{ Err error }

func (e *FetchError) Error() string { return e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }

type Subscription struct {
	ID string `json:"id"`
	// Name is the user's label; empty means the panel's title.
	Name string `json:"name"`
	// URL is empty for a list the user pasted, which is never refreshed.
	URL string `json:"url,omitempty"`
	// UserAgent overrides the one in the settings.
	UserAgent string `json:"user_agent,omitempty"`
	// HWIDScope is HWIDPanel for subscriptions that send the panel an id
	// of its own (subscription.Device.ForPanel). Those added by earlier
	// versions have none and keep sending the machine-wide id they were
	// counted under, so that their panels' device limits are not hit.
	HWIDScope string `json:"hwid_scope,omitempty"`

	Info    Info        `json:"info"`
	Format  string      `json:"format"`
	Nodes   []node.Node `json:"nodes"`
	Skipped []string    `json:"skipped,omitempty"`
	// Auto is the fingerprints of the servers the panel set up for automatic
	// selection, in order (see subscription.Result.Auto).
	Auto []string `json:"auto,omitempty"`
	// Hidden is the fingerprints of the servers the user removed from the
	// list. The panel still sends them; they stay out of the list, the
	// latency test and the switch to another server across refreshes, until
	// the user brings them back.
	Hidden []string `json:"hidden,omitempty"`

	AddedAt time.Time `json:"added_at"`
	// UpdatedAt is the last refresh that produced nodes; CheckedAt the last
	// attempt, whose error, if any, is in LastError.
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	LastError string    `json:"last_error,omitempty"`

	// fps caches the fingerprints of Nodes, which are never changed in
	// place, only replaced: fpOf is the node list they were computed for.
	fps  []string
	fpOf *node.Node
}

// HWIDPanel marks a subscription that sends its panel an id of its own.
const HWIDPanel = "panel"

// Fingerprints returns the fingerprints of sub's nodes, in their order.
// They are computed once per node list: computing one encodes the node.
func (sub *Subscription) Fingerprints() []string {
	if len(sub.Nodes) == 0 {
		return nil
	}
	if sub.fpOf != &sub.Nodes[0] || len(sub.fps) != len(sub.Nodes) {
		fps := make([]string, len(sub.Nodes))
		for i := range sub.Nodes {
			fps[i] = sub.Nodes[i].Fingerprint()
		}
		sub.fps, sub.fpOf = fps, &sub.Nodes[0]
	}
	return sub.fps
}

// IsHidden reports whether the user removed the server with fingerprint fp
// from the list.
func (sub *Subscription) IsHidden(fp string) bool { return slices.Contains(sub.Hidden, fp) }

// Insecure reports a subscription fetched over plain HTTP: its link, with
// the access token, crosses the network unencrypted.
func (sub *Subscription) Insecure() bool { return InsecureURL(sub.URL) }

// InsecureURL reports a plain HTTP subscription URL.
func InsecureURL(u string) bool {
	return len(u) >= 7 && strings.EqualFold(u[:7], "http://")
}

// Info is what the panel reported about the subscription.
type Info struct {
	Title    string `json:"title,omitempty"`
	Upload   uint64 `json:"upload"`
	Download uint64 `json:"download"`
	// Total is the traffic limit in bytes; 0 means unlimited.
	Total               uint64    `json:"total"`
	Expire              time.Time `json:"expire,omitzero"`
	UpdateIntervalHours int       `json:"update_interval_hours,omitempty"`
	SupportURL          string    `json:"support_url,omitempty"`
	WebPageURL          string    `json:"web_page_url,omitempty"`
}

// DisplayName is the user's label, else the panel's title, else a
// placeholder.
func (s *Subscription) DisplayName() string {
	switch {
	case s.Name != "":
		return s.Name
	case s.Info.Title != "":
		return s.Info.Title
	case s.URL == "":
		return "Local nodes"
	}
	return "Subscription"
}

// Selection names the node to connect. The fingerprint identifies it across
// refreshes; the name is the fallback when the panel changed its parameters.
type Selection struct {
	Subscription string `json:"subscription"`
	Fingerprint  string `json:"fingerprint"`
	Name         string `json:"name"`
}

// Change describes a modification, for the UI to refresh what it shows.
type Change struct {
	// What is one of: settings, selection, subscription-added,
	// subscription-updated, subscription-removed.
	What string
	// ID is the subscription concerned, if any.
	ID string
	// Err is set when a refresh failed.
	Err error
}

type Options struct {
	// Client fetches subscriptions; nil means a client with a 30 s timeout.
	Client *http.Client

	now func() time.Time
	// fetch downloads a subscription; legacyHWID sends the machine-wide
	// HWID (Subscription.HWIDScope).
	fetch func(ctx context.Context, url, userAgent string, legacyHWID bool) (subscription.Fetched, error)
}

// Via runs do with a client for one way to the panel, then with the next
// way while do fails (Store.SetVia).
type Via func(ctx context.Context, do func(*http.Client) error) error

type Store struct {
	path string
	opts Options
	// via is how fetches reach the panel; unset, directly with opts.Client.
	via atomic.Pointer[Via]

	fetchMu sync.Mutex // one refresh at a time; panels do not like bursts

	mu   sync.Mutex
	data fileData
	// raw is the file as last read or encoded, for keepUnknown.
	raw      []byte
	watchers []func(Change)
	seq      uint64 // counts the states encoded by modify

	// writeMu serialises writing the file, which happens outside mu so
	// that readers do not wait for the disk; written is the seq on disk.
	writeMu sync.Mutex
	written uint64
}

type fileData struct {
	Version       int            `json:"version"`
	Settings      Settings       `json:"settings"`
	Subscriptions []Subscription `json:"subscriptions"`
	Selection     *Selection     `json:"selection,omitempty"`
}

const fileVersion = 1

// Open loads the store at path; a missing file gives a fresh store. A file
// that cannot be used is moved aside and reported with ErrReset alongside a
// working store.
func Open(path string, opts Options) (*Store, error) {
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	s := &Store{path: path, opts: opts, data: fileData{Version: fileVersion, Settings: Defaults()}}
	if s.opts.fetch == nil {
		s.opts.fetch = func(ctx context.Context, url, ua string, legacyHWID bool) (subscription.Fetched, error) {
			if via := s.via.Load(); via != nil {
				return FetchVia(ctx, *via, url, ua, legacyHWID)
			}
			return subscription.FetchAs(ctx, s.opts.Client, url, ua, legacyHWID)
		}
	}

	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	loaded := fileData{Settings: Defaults()} // fields missing from the file keep their defaults
	// But the journal's, which upgrade() tells apart by their version: a
	// file's own, or none.
	loaded.Settings.Log = LogSettings{}
	var problem error
	if err := json.Unmarshal(b, &loaded); err != nil {
		problem = fmt.Errorf("unreadable: %w", err)
	} else if loaded.Version > fileVersion {
		problem = fmt.Errorf("written by a newer version (%d)", loaded.Version)
	}
	if problem != nil {
		aside := path + ".bad-" + opts.now().Format("20060102-150405")
		if rerr := os.Rename(path, aside); rerr != nil {
			return nil, fmt.Errorf("store: %s is %v and cannot be moved aside: %w", path, problem, rerr)
		}
		return s, fmt.Errorf("%w: %s is %v; moved to %s", ErrReset, filepath.Base(path), problem, filepath.Base(aside))
	}

	loaded.Settings.Log.upgrade()
	var warn error
	if set, err := loaded.Settings.normalize(); err != nil {
		// Hand-edited into something invalid, or a value of a later
		// version: keep the subscriptions, and a copy of the file, as the
		// next save overwrites the settings.
		aside := path + ".bad-" + opts.now().Format("20060102-150405")
		if werr := os.WriteFile(aside, b, 0o600); werr == nil {
			err = fmt.Errorf("%v; the file is saved as %s", err, filepath.Base(aside))
		}
		warn = fmt.Errorf("%w: invalid settings replaced with defaults: %v", ErrReset, err)
		loaded.Settings = Defaults()
	} else {
		loaded.Settings = set
	}
	loaded.Version = fileVersion
	for i := range loaded.Subscriptions {
		loaded.Subscriptions[i].Fingerprints()
	}
	s.data = loaded
	s.raw = b
	return s, warn
}

// Settings returns the current settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSettings(s.data.Settings)
}

// SetSettings validates, tidies and saves set, returning what was saved.
func (s *Store) SetSettings(set Settings) (Settings, error) {
	set, err := set.normalize()
	if err != nil {
		return Settings{}, err
	}
	err = s.modify(Change{What: "settings"}, func(d *fileData) error {
		d.Settings = set
		return nil
	})
	return cloneSettings(set), err
}

// Subscriptions returns every subscription in the user's order. Nodes are
// shared with the store and must not be modified.
func (s *Store) Subscriptions() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.Subscriptions)
}

func (s *Store) Subscription(id string) (Subscription, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Subscription{}, false
	}
	return s.data.Subscriptions[i], true
}

// AddRequest is a subscription URL or, for a pasted list, its content.
type AddRequest struct {
	Name      string
	URL       string
	Content   string
	UserAgent string
}

// Add fetches or parses the subscription and saves it only if it has nodes,
// so a mistyped URL never ends up in the list.
func (s *Store) Add(ctx context.Context, req AddRequest) (Subscription, error) {
	req.URL, req.Name, req.UserAgent = strings.TrimSpace(req.URL), strings.TrimSpace(req.Name), strings.TrimSpace(req.UserAgent)
	if (req.URL == "") == (strings.TrimSpace(req.Content) == "") {
		return Subscription{}, errors.New(`need either "url" or "content"`)
	}
	if req.URL != "" {
		if err := checkURL(req.URL); err != nil {
			return Subscription{}, err
		}
		for _, sub := range s.Subscriptions() {
			if sub.URL == req.URL {
				return Subscription{}, ErrExists
			}
		}
	}
	sub := Subscription{ID: newID(), Name: req.Name, URL: req.URL, UserAgent: req.UserAgent, AddedAt: s.opts.now()}
	if sub.URL != "" {
		sub.HWIDScope = HWIDPanel
	}
	if err := s.load(ctx, &sub, req.Content); err != nil {
		return Subscription{}, err
	}
	if unnamedPaste(sub) {
		for _, o := range s.Subscriptions() {
			if unnamedPaste(o) {
				return s.addToPaste(o.ID, sub)
			}
		}
	}
	err := s.modify(Change{What: "subscription-added", ID: sub.ID}, func(d *fileData) error {
		if sub.URL != "" && slices.ContainsFunc(d.Subscriptions, func(o Subscription) bool { return o.URL == sub.URL }) {
			return ErrExists // added by a concurrent request
		}
		d.Subscriptions = append(d.Subscriptions, sub)
		return nil
	})
	return sub, err
}

// unnamedPaste reports a pasted list without a name, shown as "Local nodes".
// Servers pasted one by one go into the first such list rather than making
// a list, all with the same name, per paste.
func unnamedPaste(sub Subscription) bool {
	return sub.URL == "" && sub.Name == "" && sub.Info.Title == ""
}

// addToPaste appends to the list id the servers of add it does not have.
func (s *Store) addToPaste(id string, add Subscription) (Subscription, error) {
	var sub Subscription
	err := s.modify(Change{What: "subscription-updated", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		sub = d.Subscriptions[i]
		var fresh []node.Node
		for _, n := range add.Nodes {
			if !slices.ContainsFunc(sub.Nodes, func(o node.Node) bool { return o.Fingerprint() == n.Fingerprint() && o.Name == n.Name }) {
				fresh = append(fresh, n)
			}
		}
		if len(fresh) == 0 {
			return ErrServersExist
		}
		// New slices: the old ones are shared with readers.
		sub.Nodes = slices.Concat(sub.Nodes, fresh)
		sub.Skipped = slices.Concat(sub.Skipped, add.Skipped)
		sub.UpdatedAt, sub.CheckedAt, sub.LastError = add.UpdatedAt, add.CheckedAt, ""
		d.Subscriptions[i] = sub
		return nil
	})
	return sub, err
}

// Edit changes a subscription's label, URL or User-Agent; nil fields are
// left alone. A new URL or User-Agent is fetched before it is saved.
type Edit struct {
	Name      *string `json:"name"`
	URL       *string `json:"url"`
	UserAgent *string `json:"user_agent"`
	// Content replaces the nodes of a pasted list.
	Content *string `json:"content"`
}

func (s *Store) Edit(ctx context.Context, id string, e Edit) (Subscription, error) {
	sub, ok := s.Subscription(id)
	if !ok {
		return Subscription{}, ErrNotFound
	}
	refetch := false
	if e.Name != nil {
		sub.Name = strings.TrimSpace(*e.Name)
	}
	if e.UserAgent != nil {
		ua := strings.TrimSpace(*e.UserAgent)
		refetch = refetch || ua != sub.UserAgent
		sub.UserAgent = ua
	}
	if e.URL != nil {
		u := strings.TrimSpace(*e.URL)
		if sub.URL == "" {
			return Subscription{}, errors.New("a pasted list has no URL; add the subscription anew")
		}
		if err := checkURL(u); err != nil {
			return Subscription{}, err
		}
		refetch = refetch || u != sub.URL
		if subscription.PanelHost(u) != subscription.PanelHost(sub.URL) {
			// Another panel: it has never seen the machine-wide id.
			sub.HWIDScope = HWIDPanel
		}
		sub.URL = u
	}
	if e.Content != nil {
		if sub.URL != "" {
			return Subscription{}, errors.New("the nodes of a URL subscription come from its panel")
		}
		if err := s.load(ctx, &sub, *e.Content); err != nil {
			return Subscription{}, err
		}
	} else if refetch && sub.URL != "" {
		if err := s.load(ctx, &sub, ""); err != nil {
			return Subscription{}, err
		}
	}
	err := s.modify(Change{What: "subscription-updated", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		if sub.URL != d.Subscriptions[i].URL &&
			slices.ContainsFunc(d.Subscriptions, func(o Subscription) bool { return o.URL == sub.URL }) {
			return ErrExists
		}
		d.Subscriptions[i] = sub
		repointSelection(d, &sub)
		return nil
	})
	return sub, err
}

// Remove deletes a subscription, and the selection if it pointed there.
func (s *Store) Remove(id string) error {
	return s.modify(Change{What: "subscription-removed", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		d.Subscriptions = slices.Delete(d.Subscriptions, i, i+1)
		if d.Selection != nil && d.Selection.Subscription == id {
			d.Selection = nil
		}
		return nil
	})
}

// Move puts subscription id at position index in the list.
func (s *Store) Move(id string, index int) error {
	return s.modify(Change{What: "subscription-updated", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		sub := d.Subscriptions[i]
		d.Subscriptions = slices.Delete(d.Subscriptions, i, i+1)
		index = min(max(index, 0), len(d.Subscriptions))
		d.Subscriptions = slices.Insert(d.Subscriptions, index, sub)
		return nil
	})
}

// maxHidden bounds the servers hidden in one subscription, so that a
// request cannot grow the file without end.
const maxHidden = 10000

// SetHidden removes the servers with fingerprints fps from subscription
// id's list (hidden) or brings them back. A server to hide must be in the
// list; one to bring back need not be, so that what a panel dropped can be
// cleared too. The fingerprints stay when a refresh replaces the nodes.
func (s *Store) SetHidden(id string, fps []string, hidden bool) (Subscription, error) {
	var sub Subscription
	err := s.modify(Change{What: "subscription-updated", ID: id}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound
		}
		cur := &d.Subscriptions[i]
		// A new slice: the old one is shared with readers.
		out := slices.Clone(cur.Hidden)
		for _, fp := range fps {
			has := slices.Contains(out, fp)
			switch {
			case hidden && !has:
				if !slices.Contains(cur.Fingerprints(), fp) {
					return fmt.Errorf("subscription has no node %s", fp)
				}
				out = append(out, fp)
			case !hidden && has:
				out = slices.DeleteFunc(out, func(h string) bool { return h == fp })
			}
		}
		if len(out) > maxHidden {
			return errors.New("too many hidden servers")
		}
		if len(out) == 0 {
			out = nil
		}
		cur.Hidden = out
		sub = *cur
		return nil
	})
	return sub, err
}

// Refresh downloads subscription id again. On failure the old nodes are
// kept and the error is recorded.
func (s *Store) Refresh(ctx context.Context, id string) (Subscription, error) {
	sub, ok := s.Subscription(id)
	if !ok {
		return Subscription{}, ErrNotFound
	}
	if sub.URL == "" {
		return sub, errors.New("a pasted list has nothing to refresh")
	}
	fetchErr := s.load(ctx, &sub, "")
	sub.CheckedAt = s.opts.now()
	if fetchErr != nil {
		sub.LastError = fetchErr.Error()
	}
	err := s.modify(Change{What: "subscription-updated", ID: id, Err: fetchErr}, func(d *fileData) error {
		i := s.index(id)
		if i < 0 {
			return ErrNotFound // removed while fetching
		}
		cur := &d.Subscriptions[i]
		if fetchErr != nil {
			// The nodes stay; what the panel said about the subscription is
			// new, if it said anything.
			cur.Info, cur.CheckedAt, cur.LastError = sub.Info, sub.CheckedAt, sub.LastError
			return nil
		}
		// Keep edits made while fetching; take only what the fetch produced.
		cur.Info, cur.Format, cur.Nodes, cur.Skipped, cur.Auto = sub.Info, sub.Format, sub.Nodes, sub.Skipped, sub.Auto
		cur.UpdatedAt, cur.CheckedAt, cur.LastError = sub.UpdatedAt, sub.CheckedAt, ""
		repointSelection(d, cur)
		sub = *cur
		return nil
	})
	return sub, errors.Join(fetchErr, err)
}

// SetVia makes subscriptions reach their panels through via (the service
// adds the tunnel while it is up); nil goes back to Options.Client.
func (s *Store) SetVia(via Via) {
	if via == nil {
		s.via.Store(nil)
		return
	}
	s.via.Store(&via)
}

// FetchVia downloads a subscription as subscription.FetchAs does, trying
// the ways via offers until one gets servers. A panel that told about the
// subscription (its headers) answered, even with an error such as a
// message instead of servers: another way would get the same.
func FetchVia(ctx context.Context, via Via, rawURL, userAgent string, legacyHWID bool) (subscription.Fetched, error) {
	var f subscription.Fetched
	var answer error
	err := via(ctx, func(c *http.Client) error {
		var err error
		f, err = subscription.FetchAs(ctx, c, rawURL, userAgent, legacyHWID)
		if err != nil && len(f.Nodes) == 0 && f.Info == (subscription.Info{}) {
			return err
		}
		answer = err
		return nil
	})
	if err != nil {
		return subscription.Fetched{}, err
	}
	return f, answer
}

// load fills sub's nodes from its URL or, when content is set, from content.
func (s *Store) load(ctx context.Context, sub *Subscription, content string) error {
	var f subscription.Fetched
	var err error
	if content != "" {
		f.Result, err = subscription.Parse([]byte(content))
	} else {
		s.fetchMu.Lock()
		ua := sub.UserAgent
		if ua == "" {
			ua = s.Settings().Updates.UserAgent
		}
		f, err = s.opts.fetch(ctx, sub.URL, ua, sub.HWIDScope != HWIDPanel)
		s.fetchMu.Unlock()
		if err != nil && len(f.Nodes) == 0 {
			// A panel that sent a message instead of servers (subscription
			// expired, device limit) still says until when and where its
			// support is.
			if f.Info != (subscription.Info{}) {
				sub.Info = infoOf(f.Info)
			}
			return &FetchError{err}
		}
	}
	if err != nil && len(f.Nodes) == 0 {
		return err
	}
	sub.Info = infoOf(f.Info)
	sub.Format, sub.Nodes, sub.Skipped, sub.Auto = string(f.Format), f.Nodes, nil, f.Auto
	for _, sk := range f.Skipped {
		sub.Skipped = append(sub.Skipped, sk.String())
	}
	sub.UpdatedAt = s.opts.now()
	sub.CheckedAt, sub.LastError = sub.UpdatedAt, ""
	return nil
}

func infoOf(i subscription.Info) Info {
	return Info{
		Title: i.Title, Upload: i.Upload, Download: i.Download, Total: i.Total,
		Expire: i.Expire, UpdateIntervalHours: int(i.UpdateInterval.Hours()),
		SupportURL: i.SupportURL, WebPageURL: i.WebPageURL,
	}
}

// Select makes node fingerprint of subscription subID the one to connect.
// Select selects a node. name tells apart nodes that differ only in name,
// which share a fingerprint; it may be empty.
func (s *Store) Select(subID, fingerprint, name string) (Selection, error) {
	var sel Selection
	err := s.modify(Change{What: "selection", ID: subID}, func(d *fileData) error {
		i := s.index(subID)
		if i < 0 {
			return ErrNotFound
		}
		n, ok := findNode(&d.Subscriptions[i], fingerprint, name)
		if !ok || n.Fingerprint() != fingerprint {
			return fmt.Errorf("subscription has no node %s", fingerprint)
		}
		sel = Selection{Subscription: subID, Fingerprint: fingerprint, Name: n.Name}
		d.Selection = &sel
		return nil
	})
	return sel, err
}

// Selected returns the selection and its node. ok is false when nothing is
// selected or the node has disappeared from its subscription; the selection
// is still returned in the latter case so the UI can say which one.
func (s *Store) Selected() (sel Selection, n node.Node, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Selection == nil {
		return Selection{}, node.Node{}, false
	}
	sel = *s.data.Selection
	if i := s.index(sel.Subscription); i >= 0 {
		n, ok = findNode(&s.data.Subscriptions[i], sel.Fingerprint, sel.Name)
	}
	return sel, n, ok
}

// repointSelection follows the selected node through a refresh: same
// fingerprint, else the only node with the same name.
func repointSelection(d *fileData, sub *Subscription) {
	sel := d.Selection
	if sel == nil || sel.Subscription != sub.ID {
		return
	}
	if n, ok := findNode(sub, sel.Fingerprint, sel.Name); ok {
		sel.Fingerprint, sel.Name = n.Fingerprint(), n.Name
	}
}

// findNode finds a node by fingerprint, preferring the one named name when
// several share it, else by name alone if that is unambiguous.
func findNode(sub *Subscription, fingerprint, name string) (node.Node, bool) {
	nodes, fps := sub.Nodes, sub.Fingerprints()
	var byFP *node.Node
	for i := range nodes {
		if fps[i] != fingerprint {
			continue
		}
		if nodes[i].Name == name {
			return nodes[i], true
		}
		if byFP == nil {
			byFP = &nodes[i]
		}
	}
	if byFP != nil {
		return *byFP, true
	}
	if name == "" {
		return node.Node{}, false
	}
	var match *node.Node
	for i := range nodes {
		if nodes[i].Name == name {
			if match != nil {
				return node.Node{}, false // ambiguous
			}
			match = &nodes[i]
		}
	}
	if match == nil {
		return node.Node{}, false
	}
	return *match, true
}

// modify applies f and saves. When f fails nothing changes; when saving
// fails the change is undone too, unless a later one was made meanwhile,
// which then carries it to the file.
//
// The new state is encoded under mu, in the order of the changes, and
// written outside it, so that readers never wait for the disk; a write
// never replaces a later state on disk with an earlier one.
func (s *Store) modify(c Change, f func(*fileData) error) error {
	s.mu.Lock()
	orig, origRaw := s.data, s.raw
	s.data = cloneData(s.data)
	err := f(&s.data)
	var b []byte
	if err == nil {
		for i := range s.data.Subscriptions {
			s.data.Subscriptions[i].Fingerprints()
		}
		b, err = s.encode()
	}
	if err != nil {
		s.data = orig
		s.mu.Unlock()
		return err
	}
	s.raw = b
	s.seq++
	seq := s.seq
	watchers := s.watchers
	s.mu.Unlock()

	if err := s.write(seq, b); err != nil {
		s.mu.Lock()
		if s.seq == seq {
			s.data, s.raw = orig, origRaw
		}
		s.mu.Unlock()
		return err
	}
	for _, w := range watchers {
		w(c)
	}
	return nil
}

// Watch registers f to be called after every modification, outside the
// store's lock, on the goroutine that made it.
func (s *Store) Watch(f func(Change)) {
	s.mu.Lock()
	s.watchers = append(slices.Clip(s.watchers), f)
	s.mu.Unlock()
}

func (s *Store) index(id string) int {
	return slices.IndexFunc(s.data.Subscriptions, func(sub Subscription) bool { return sub.ID == id })
}

// encode returns the file for the current state, with the fields of later
// versions carried over. Call it with mu held.
func (s *Store) encode() ([]byte, error) {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return nil, err
	}
	return keepUnknown(b, s.raw, reflect.TypeOf(s.data))
}

// write saves state seq atomically: a crash leaves the old or the new
// file. A state older than the one on disk is not written.
func (s *Store) write(seq uint64, b []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if seq <= s.written {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := fsutil.WriteAtomic(s.path, b, 0o600); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	s.written = seq
	return nil
}

// cloneData copies what modify callbacks may change in place; nodes are
// never modified in place, only replaced.
func cloneData(d fileData) fileData {
	d.Settings = cloneSettings(d.Settings)
	d.Subscriptions = slices.Clone(d.Subscriptions)
	if d.Selection != nil {
		sel := *d.Selection
		d.Selection = &sel
	}
	return d
}

// cloneSettings copies every slice and map of s, so that the copy can be
// changed, decoded into among others, without touching s.
func cloneSettings(s Settings) Settings {
	cloneDeep(reflect.ValueOf(&s).Elem())
	return s
}

// cloneDeep replaces the slices, maps and pointers reachable from v with
// copies. Nil stays nil and empty stays empty, as JSON tells them apart.
func cloneDeep(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				cloneDeep(f)
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(c, v)
		for i := range c.Len() {
			cloneDeep(c.Index(i))
		}
		v.Set(c)
	case reflect.Map:
		if v.IsNil() {
			return
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(it.Value())
			cloneDeep(e)
			c.SetMapIndex(it.Key(), e)
		}
		v.Set(c)
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		c := reflect.New(v.Type().Elem())
		c.Elem().Set(v.Elem())
		cloneDeep(c.Elem())
		v.Set(c)
	}
}

func checkURL(u string) error {
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return errors.New("a subscription URL starts with https:// or http://")
	}
	if strings.ContainsAny(u, " \r\n\t") {
		return errors.New("a subscription URL cannot contain spaces")
	}
	return nil
}

func newID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
