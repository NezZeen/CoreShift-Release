package mobile

import (
	"path/filepath"
	"testing"
	"time"

	"coreshift/engine/internal/store"
)

const gb = 1 << 30

func sub(id, name string, info store.Info) store.Subscription {
	return store.Subscription{ID: id, Name: name, Info: info}
}

// The steps and the words are the app's (sub_alerts.dart): Android keeps
// one notification per title, so they must match to the letter.
func TestSubWarnings(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	for _, c := range []struct {
		name  string
		info  store.Info
		title string
		body  string
		level int
	}{
		{"ended", store.Info{Expire: now.Add(-time.Hour)}, "Подписка «A» закончилась", "Продлите её у провайдера: без этого серверы не работают.", 3},
		{"tonight", store.Info{Expire: now.Add(6 * time.Hour)}, "Подписка «A» закончится сегодня", "Продлите её у провайдера, чтобы VPN не отключился.", 2},
		{"tomorrow", store.Info{Expire: now.Add(20 * time.Hour)}, "Подписка «A» закончится завтра", "Продлите её у провайдера, чтобы VPN не отключился.", 2},
		{"in 2 days", store.Info{Expire: now.Add(36 * time.Hour)}, "Подписка «A» закончится через 2 дня", "Продлите её у провайдера заранее.", 1},
		{"in 3 days", store.Info{Expire: now.Add(71 * time.Hour)}, "Подписка «A» закончится через 3 дня", "Продлите её у провайдера заранее.", 1},
		{"traffic over", store.Info{Total: 100 * gb, Download: 100 * gb}, "Трафик подписки «A» закончился", "Докупите трафик у провайдера или дождитесь его обновления.", 3},
		{"traffic 90%", store.Info{Total: 100 * gb, Download: 85 * gb, Upload: 7 * gb}, "Трафик подписки «A» почти израсходован", "Осталось 8 ГБ из 100 ГБ.", 1},
	} {
		w := subWarnings([]store.Subscription{sub("s1", "A", c.info)}, now)
		if len(w) != 1 || w[0].title != c.title || w[0].body != c.body || w[0].level != c.level {
			t.Errorf("%s: %+v", c.name, w)
		}
	}
	// Far off, plenty left, or no limits: nothing.
	quiet := []store.Subscription{
		sub("a", "A", store.Info{Expire: now.Add(73 * time.Hour)}),
		sub("b", "B", store.Info{Total: 100 * gb, Download: 50 * gb}),
		sub("c", "C", store.Info{}),
	}
	if w := subWarnings(quiet, now); len(w) != 0 {
		t.Errorf("quiet: %+v", w)
	}
	// The placeholder reads as the app names it.
	if w := subWarnings([]store.Subscription{{ID: "p", URL: "https://x.example/s", Info: store.Info{Expire: now.Add(-time.Hour)}}}, now); len(w) != 1 || w[0].title != "Подписка «Подписка» закончилась" {
		t.Errorf("placeholder: %+v", w)
	}
}

func TestFormatQuota(t *testing.T) {
	for b, want := range map[float64]string{
		100 * gb:        "100 ГБ",
		1.5 * gb:        "1.5 ГБ",
		512 * (1 << 20): "512 МБ",
		2048 * gb:       "2 ТБ",
		300 * (1 << 10): "300 КБ",
		123.456 * gb:    "123 ГБ",
	} {
		if got := formatQuota(b); got != want {
			t.Errorf("formatQuota(%v) = %q, want %q", b, got, want)
		}
	}
}

// Each step once; a later step again; a renewal starts over; the remembered
// steps outlive the engine.
func TestSubAlertsOncePerStep(t *testing.T) {
	a := subAlerts{path: filepath.Join(t.TempDir(), "sub_warned.json")}
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	expire := now.Add(60 * time.Hour)
	subs := func() []store.Subscription { return []store.Subscription{sub("s", "A", store.Info{Expire: expire})} }
	if got := a.due(subs(), now); len(got) != 1 || got[0].level != 1 {
		t.Fatalf("first: %+v", got)
	}
	if got := a.due(subs(), now.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("the same step again: %+v", got)
	}
	// A new engine, the same file.
	b := subAlerts{path: a.path}
	if got := b.due(subs(), now.Add(48*time.Hour)); len(got) != 1 || got[0].level != 2 {
		t.Fatalf("within a day: %+v", got)
	}
	// Renewed: a month on, quiet; near its new end, warned again.
	expire = now.Add(30 * 24 * time.Hour)
	if got := b.due(subs(), now.Add(49*time.Hour)); len(got) != 0 {
		t.Fatalf("renewed: %+v", got)
	}
	if got := b.due(subs(), expire.Add(-50*time.Hour)); len(got) != 1 || got[0].level != 1 {
		t.Fatalf("near the new end: %+v", got)
	}
}
