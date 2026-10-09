package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"coreshift/engine/internal/selfupdate"
	"coreshift/engine/internal/store"
)

// whitelist is a network that, while refusing, resets every direct
// connection, as a mobile operator's white list does; the tunnel (the
// fake core, FAKECORE_FORWARD) still reaches the panel and GitHub, both
// served by srv under the .test zone.
type whitelist struct {
	srv      *httptest.Server
	refusing atomic.Bool
	direct   atomic.Int32 // direct connections tried
	subs     atomic.Int32 // subscription requests that reached the panel
	checks   atomic.Int32 // update checks that reached GitHub
}

func newWhitelist(t *testing.T) *whitelist {
	w := &whitelist{}
	w.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/release") {
			w.checks.Add(1)
			return
		}
		w.subs.Add(1)
		io.WriteString(rw, "trojan://pw@203.0.113.5:443?sni=t.example.com#Trojan\n")
	}))
	t.Cleanup(w.srv.Close)
	t.Setenv("FAKECORE_FORWARD", "test="+w.srv.Listener.Addr().String())
	return w
}

func (w *whitelist) dial(ctx context.Context, network, _ string) (net.Conn, error) {
	w.direct.Add(1)
	if w.refusing.Load() {
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}
	var d net.Dialer
	return d.DialContext(ctx, network, w.srv.Listener.Addr().String())
}

func whitelistHarness(t *testing.T, w *whitelist) (*harness, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) {
		c.Store = st
		c.dialDirect = w.dial
		c.retryDelay = 50 * time.Millisecond
		c.SelfUpdate, c.AnnounceUpdates = false, true
		c.updateFirstCheck, c.updateTick = time.Hour, time.Hour
		c.checkRelease = func(ctx context.Context, client *http.Client, _ selfupdate.Source) (selfupdate.Release, error) {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://github.test/release", nil)
			resp, err := client.Do(req)
			if err != nil {
				return selfupdate.Release{}, err
			}
			resp.Body.Close()
			return selfupdate.Release{}, nil // nothing newer
		}
	})
	return h, st
}

// Connected, a subscription the direct way cannot reach comes through the
// tunnel; the direct way is tried first.
func TestSubscriptionRefreshFallsBackToTheTunnel(t *testing.T) {
	w := newWhitelist(t)
	h, st := whitelistHarness(t, w)
	w.refusing.Store(true)
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	sub, err := st.Add(context.Background(), store.AddRequest{URL: "http://panel.test/sub/secret-token"})
	if err != nil || len(sub.Nodes) != 1 {
		t.Fatalf("add: %v", err)
	}
	sub, err = st.Refresh(context.Background(), sub.ID)
	if err != nil || sub.LastError != "" {
		t.Fatalf("refresh: %v", err)
	}
	if w.direct.Load() != 2 || w.subs.Load() != 2 {
		t.Errorf("%d direct attempts and %d requests at the panel, want 2 and 2", w.direct.Load(), w.subs.Load())
	}
	// The preview before adding goes the same way.
	if _, err := h.svc.fetchSubscription(context.Background(), "http://panel.test/sub/secret-token", ""); err != nil {
		t.Errorf("preview: %v", err)
	}
}

// Disconnected, the one direct attempt's failure is retried once a little
// after the connection comes up, through the tunnel; a refresh that worked
// is not, nor is a retry that failed again.
func TestRefreshAndUpdateCheckRetriedAfterConnect(t *testing.T) {
	w := newWhitelist(t)
	h, st := whitelistHarness(t, w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.svc.RunAppUpdates(ctx)

	sub, err := st.Add(context.Background(), store.AddRequest{URL: "http://panel.test/sub/secret-token"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	ok, err := st.Add(context.Background(), store.AddRequest{URL: "http://panel.test/other"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := st.Refresh(context.Background(), ok.ID); err != nil {
		t.Fatal(err)
	}
	w.refusing.Store(true)
	_, err = st.Refresh(context.Background(), sub.ID)
	if !retryable(err) {
		t.Fatalf("refresh without a route: %v, retryable %v", err, retryable(err))
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("the error carries the link: %v", err)
	}
	h.svc.checkAppUpdate(ctx)
	if u := h.svc.AppUpdateState(); u.State != UpdateError || w.checks.Load() != 0 {
		t.Fatalf("update check without a route: %+v", u)
	}
	subsBefore := w.subs.Load()

	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the retries", func() bool {
		s, _ := st.Subscription(sub.ID)
		return s.LastError == "" && w.checks.Load() == 1 && h.svc.AppUpdateState().State == UpdateIdle
	})
	if n := w.subs.Load() - subsBefore; n != 1 {
		t.Errorf("%d subscription requests after connecting, want 1 (the failed one only)", n)
	}

	// Nothing is left to retry: reconnecting asks nothing again.
	h.svc.Disconnect()
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := w.subs.Load() - subsBefore; n != 1 || w.checks.Load() != 1 {
		t.Errorf("after reconnecting: %d subscription requests, %d update checks; want 1 and 1", n, w.checks.Load())
	}
}

// A failure through the tunnel, or one that is not about the route, waits
// for the usual schedule.
func TestOnlyUntunneledRouteFailuresAreRetried(t *testing.T) {
	dial := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	lookup := &net.DNSError{Err: "no such host", Name: "panel.example", IsNotFound: true}
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&untunneled{&store.FetchError{Err: dial}}, true},
		{&store.FetchError{Err: &untunneled{lookup}}, true},
		{dial, false}, // through the tunnel as well
		{&untunneled{errors.New("fetch subscription: server returned 403 Forbidden")}, false},
		{&untunneled{&net.OpError{Op: "read", Net: "tcp", Err: errors.New("i/o timeout")}}, false},
		{nil, false},
	} {
		if got := retryable(c.err); got != c.want {
			t.Errorf("retryable(%v) = %v", c.err, got)
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
