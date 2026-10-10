package service

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/store"
)

func TestAnnounceLine(t *testing.T) {
	m := announceLine(msg.Raw("Мой VPN"), "Профилактика\nв пятницу")
	if got, want := m.String(), "объявление от «Мой VPN»: Профилактика в пятницу"; got != want || m.Code != "announce.line" || m.Args["text"] != "Профилактика в пятницу" {
		t.Errorf("line = %q (%+v)", got, m)
	}
	long := announceLine(msg.Raw("S"), strings.Repeat("ж", 200)).String()
	if want := "объявление от «S»: " + strings.Repeat("ж", 80) + "…"; long != want {
		t.Errorf("long line = %q", long)
	}
}

// A new announcement is journaled once, in one line without the
// subscription's link; the same one again, or none, says nothing.
func TestAnnouncementIsJournaledOnce(t *testing.T) {
	var mu sync.Mutex
	announce := "base64:" + base64.StdEncoding.EncodeToString([]byte("Скидка 20% до воскресенья"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if announce != "" {
			w.Header().Set("Announce", announce)
		}
		w.Write([]byte("trojan://pw@203.0.113.5:443?sni=t.example.com#Trojan\n"))
	}))
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.json"), store.Options{Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) { c.Store = st })

	lines := func() []string {
		var out []string
		for len(h.events) > 0 {
			if e := <-h.events; e.Kind == "action" && e.Source == "подписка" {
				out = append(out, e.Line)
			}
		}
		return out
	}
	sub, err := st.Add(t.Context(), store.AddRequest{Name: "Мой VPN", URL: srv.URL + "/sub/SECRET-TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	want := "объявление от «Мой VPN»: Скидка 20% до воскресенья"
	if got := lines(); len(got) != 1 || got[0] != want {
		t.Fatalf("on add: %q", got)
	}
	if _, err := st.Refresh(t.Context(), sub.ID); err != nil {
		t.Fatal(err)
	}
	if got := lines(); len(got) != 0 {
		t.Errorf("the same text again: %q", got)
	}

	mu.Lock()
	announce = "Новый сервер в Финляндии"
	mu.Unlock()
	st.Refresh(t.Context(), sub.ID)
	if got := lines(); len(got) != 1 || got[0] != "объявление от «Мой VPN»: Новый сервер в Финляндии" {
		t.Errorf("new text: %q", got)
	}

	mu.Lock()
	announce = ""
	mu.Unlock()
	st.Refresh(t.Context(), sub.ID)
	if got := lines(); len(got) != 0 {
		t.Errorf("cleared: %q", got)
	}
	for _, l := range append(lines(), want) {
		if strings.Contains(l, "SECRET-TOKEN") {
			t.Errorf("the link is in the journal: %q", l)
		}
	}
}
