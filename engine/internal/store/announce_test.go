package store

import (
	"context"
	"testing"

	"coreshift/engine/internal/subscription"
)

// The provider's announcement is kept with the subscription across
// restarts, replaced by the next one and cleared when the panel stops
// sending it.
func TestAnnouncementFollowsTheRefresh(t *testing.T) {
	f := newFixture(t)
	f.panel.set(subURL, pasted)
	f.panel.info = subscription.Info{Announce: "Профилактика ночью", AnnounceURL: "https://example.com/status"}
	s := f.open(t)
	ctx := context.Background()
	sub, err := s.Add(ctx, AddRequest{URL: subURL})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Info.Announce != "Профилактика ночью" || sub.Info.AnnounceURL != "https://example.com/status" {
		t.Fatalf("added: %+v", sub.Info)
	}

	// Kept across a restart.
	s2 := f.open(t)
	if got, _ := s2.Subscription(sub.ID); got.Info.Announce != "Профилактика ночью" || got.Info.AnnounceURL != "https://example.com/status" {
		t.Errorf("after restart: %+v", got.Info)
	}

	f.panel.info = subscription.Info{Announce: "Новый сервер"}
	got, err := s2.Refresh(ctx, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Info.Announce != "Новый сервер" || got.Info.AnnounceURL != "" {
		t.Errorf("replaced: %+v", got.Info)
	}

	f.panel.info = subscription.Info{}
	got, err = s2.Refresh(ctx, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Info.Announce != "" || got.Info.AnnounceURL != "" {
		t.Errorf("not cleared: %+v", got.Info)
	}
	if again, _ := s2.Subscription(sub.ID); again.Info.Announce != "" {
		t.Errorf("not cleared in the store: %+v", again.Info)
	}
}
