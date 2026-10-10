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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

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
	// Announce is the provider's announcement, AnnounceURL its link.
	Announce    string `json:"announce,omitempty"`
	AnnounceURL string `json:"announce_url,omitempty"`
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
