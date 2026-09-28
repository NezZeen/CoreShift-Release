package service

import (
	"context"
	"testing"
	"time"
)

func waitGone(s *Service, first, grace time.Duration) <-chan bool {
	ch := make(chan bool, 1)
	go func() { ch <- s.WaitAppGone(context.Background(), first, grace) }()
	return ch
}

func TestAppGoneWhenNoneEverAttaches(t *testing.T) {
	s := &Service{}
	select {
	case gone := <-waitGone(s, 50*time.Millisecond, time.Hour):
		if !gone {
			t.Fatal("WaitAppGone = false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no app, yet the daemon keeps waiting")
	}
}

func TestAppGoneAfterItsStreamEnds(t *testing.T) {
	s := &Service{}
	detach := s.AttachApp()
	gone := waitGone(s, 50*time.Millisecond, 100*time.Millisecond)
	select {
	case <-gone:
		t.Fatal("gone while the app is attached")
	case <-time.After(300 * time.Millisecond):
	}
	// A restarting app comes back within the grace period.
	detach()
	detach() // twice is harmless
	time.Sleep(30 * time.Millisecond)
	again := s.AttachApp()
	select {
	case <-gone:
		t.Fatal("gone although the app came back")
	case <-time.After(300 * time.Millisecond):
	}
	start := time.Now()
	again()
	select {
	case ok := <-gone:
		if !ok {
			t.Fatal("WaitAppGone = false")
		}
		if d := time.Since(start); d < 90*time.Millisecond {
			t.Fatalf("gone after %v, before the grace period", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the app is closed, yet the daemon keeps waiting")
	}
}

func TestAppWatchEndsWithContext(t *testing.T) {
	s := &Service{}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan bool, 1)
	go func() { ch <- s.WaitAppGone(ctx, time.Hour, time.Hour) }()
	cancel()
	if <-ch {
		t.Fatal("WaitAppGone = true after cancel")
	}
}
