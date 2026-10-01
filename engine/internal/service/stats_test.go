package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatsKeepDaysApartAndSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	s := openStats(path)
	s.add(now, 100, 1000)
	s.add(now.Add(time.Hour), 5, 50)
	s.add(now.AddDate(0, 0, -2), 7, 70)
	s.add(now, 0, 0)   // nothing to count
	s.add(now, -5, -5) // a counter that went back counts for nothing
	s.flush(now)

	got := openStats(path).report(now, 4)
	want := []StatsDay{{Date: "2026-09-28"}, {Date: "2026-09-29", Up: 7, Down: 70}, {Date: "2026-09-30"}, {Date: "2026-10-01", Up: 105, Down: 1050}}
	if len(got.Days) != len(want) {
		t.Fatalf("days = %+v", got.Days)
	}
	for i := range want {
		if got.Days[i] != want[i] {
			t.Errorf("day %d = %+v, want %+v", i, got.Days[i], want[i])
		}
	}
}

func TestStatsDropOldDaysAndSurviveAGarbledFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	s := openStats(path)
	s.add(now.AddDate(0, 0, -(statsKeepDays+5)), 1, 1)
	s.add(now, 2, 2)
	s.flush(now)
	if d := openStats(path).report(now, statsKeepDays+10).Days; d[0].Up != 0 || d[len(d)-1].Up != 2 {
		t.Errorf("old day kept or today lost: first %+v last %+v", d[0], d[len(d)-1])
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d := openStats(path).report(now, 3).Days; len(d) != 3 || d[2].Up != 0 {
		t.Errorf("a garbled file must start over: %+v", d)
	}
}

func TestTrafficIsCountedPerDay(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.connect(t, hy2Link); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if d := h.svc.Stats(7).Days; d[len(d)-1].Down > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	h.svc.Disconnect() // writes the totals out
	st := h.svc.Stats(7)
	if len(st.Days) != 7 {
		t.Fatalf("days = %d", len(st.Days))
	}
	today := st.Days[6]
	if today.Date != time.Now().Format(dayLayout) || today.Down <= 0 || today.Up <= 0 {
		t.Errorf("today = %+v", today)
	}
	if _, err := os.Stat(filepath.Join(h.svc.cfg.DataDir, "traffic.json")); err != nil {
		t.Errorf("totals not saved: %v", err)
	}
}
