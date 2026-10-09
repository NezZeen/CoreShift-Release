package store

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// twoWays is a Via with a way that cannot connect (an operator's white
// list resetting it) and one that can, tried in that order unless
// openFirst; it counts the attempts.
type twoWays struct {
	openFirst bool
	tries     int
}

func (w *twoWays) via(_ context.Context, do func(*http.Client) error) error {
	refused := &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
		},
	}}
	open := &http.Client{Transport: &http.Transport{}}
	clients := []*http.Client{refused, open}
	if w.openFirst {
		clients = []*http.Client{open, refused}
	}
	var errs []error
	for _, c := range clients {
		w.tries++
		err := do(c)
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// A subscription the first way cannot reach is fetched the next way, when
// it is added and when it is refreshed.
func TestRefreshTakesTheNextWay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "trojan://pw@203.0.113.5:443?sni=t.example.com#A\n")
	}))
	defer srv.Close()
	st, err := Open(filepath.Join(t.TempDir(), "store.json"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ways := &twoWays{}
	st.SetVia(ways.via)
	sub, err := st.Add(context.Background(), AddRequest{URL: srv.URL + "/sub/secret-token"})
	if err != nil || len(sub.Nodes) != 1 {
		t.Fatalf("add: %v, %d nodes", err, len(sub.Nodes))
	}
	sub, err = st.Refresh(context.Background(), sub.ID)
	if err != nil || len(sub.Nodes) != 1 || sub.LastError != "" {
		t.Fatalf("refresh: %v, %+v", err, sub)
	}
	if ways.tries != 4 {
		t.Errorf("%d attempts for an add and a refresh, want 2 each", ways.tries)
	}
}

// A panel that answered, even with a message instead of servers, is not
// asked again another way; one that did not answer is, and the error
// keeps the subscription's link out.
func TestFetchViaStopsAtThePanelsAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/gone") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Subscription-Userinfo", "upload=0; download=0; total=0; expire=1")
		io.WriteString(w, "trojan://pw@0.0.0.0:1#Subscription expired\n")
	}))
	defer srv.Close()

	ways := &twoWays{openFirst: true}
	f, err := FetchVia(context.Background(), ways.via, srv.URL+"/sub/secret-token", "", false)
	if err == nil || !strings.Contains(err.Error(), "Subscription expired") || f.Info.Expire.IsZero() || ways.tries != 1 {
		t.Errorf("expired: err %v, info %+v after %d attempts, want the message and the info after 1", err, f.Info, ways.tries)
	}

	ways = &twoWays{openFirst: true}
	_, err = FetchVia(context.Background(), ways.via, srv.URL+"/sub/secret-token/gone", "", false)
	if err == nil || ways.tries != 2 {
		t.Errorf("missing: err %v after %d attempts, want 2", err, ways.tries)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("the error carries the link: %v", err)
	}
}
