package store

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestHiddenServersStayHiddenAcrossRefresh(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted)
	s := f.open(t)
	sub, err := s.Add(context.Background(), AddRequest{URL: subURL})
	if err != nil {
		t.Fatal(err)
	}
	fpA, fpB := sub.Nodes[0].Fingerprint(), sub.Nodes[1].Fingerprint()
	if _, err := s.SetHidden(sub.ID, []string{"nope"}, true); err == nil {
		t.Error("hid a server the subscription does not have")
	}
	if _, err := s.SetHidden("nope", []string{fpA}, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown subscription: %v", err)
	}
	got, err := s.SetHidden(sub.ID, []string{fpB, fpB}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Hidden, []string{fpB}) || !got.IsHidden(fpB) || got.IsHidden(fpA) {
		t.Fatalf("hidden = %v", got.Hidden)
	}

	// The panel sends Berlin again, and a new server: Berlin stays hidden.
	f.panel.set(subURL, pasted+"\n"+nodeA2)
	if _, err := s.Refresh(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	// Read back from the file, too.
	s2, err := Open(f.path, f.options())
	if err != nil {
		t.Fatal(err)
	}
	cur, _ := s2.Subscription(sub.ID)
	if len(cur.Nodes) != 3 || !cur.IsHidden(fpB) || cur.IsHidden(fpA) {
		t.Fatalf("after refresh: %d nodes, hidden %v", len(cur.Nodes), cur.Hidden)
	}

	// Brought back; a server the panel dropped can be cleared as well.
	got, err = s2.SetHidden(sub.ID, []string{fpB, "gone"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hidden != nil {
		t.Errorf("hidden after restoring = %v", got.Hidden)
	}
	if b, _ := os.ReadFile(f.path); strings.Contains(string(b), `"hidden"`) {
		t.Error("an empty list is still written to the file")
	}
}

// Hiding does not change the list readers already hold.
func TestSetHiddenCopiesTheList(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	sub, err := s.Add(context.Background(), AddRequest{Content: pasted})
	if err != nil {
		t.Fatal(err)
	}
	fpA, fpB := sub.Nodes[0].Fingerprint(), sub.Nodes[1].Fingerprint()
	first, _ := s.SetHidden(sub.ID, []string{fpA}, true)
	if _, err := s.SetHidden(sub.ID, []string{fpB}, true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(first.Hidden, []string{fpA}) {
		t.Errorf("an earlier copy changed: %v", first.Hidden)
	}
}
