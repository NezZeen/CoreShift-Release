package mobile

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"coreshift/engine/internal/store"
)

// A subscription running out, by date or by traffic: what the engine tells
// Android while the app is closed. The titles and the steps are those of
// the app (app/lib/state/app_state/sub_alerts.dart), which shows the same
// warnings as banners: Android keeps one notification per title, so a
// warning the app shows too replaces itself rather than coming twice.
type subWarning struct {
	key   string // the subscription and "traffic" or "expire"
	term  string // the limit or the date: a renewal starts the steps again
	level int    // 1 soon, 2 within a day, 3 already over
	title string
	body  string
}

// placeholderNames are the app's names for the engine's placeholders.
var placeholderNames = map[string]string{"Local nodes": "Мои серверы", "Subscription": "Подписка"}

// subWarnings returns the warnings due at now, as the app words them.
func subWarnings(subs []store.Subscription, now time.Time) []subWarning {
	var out []subWarning
	for _, s := range subs {
		dn := s.DisplayName()
		if p, ok := placeholderNames[dn]; ok {
			dn = p
		}
		name := "«" + dn + "»"
		i := s.Info
		if !i.Expire.IsZero() {
			w := subWarning{key: s.ID + "/expire", term: strconv.FormatInt(i.Expire.UnixMilli(), 10)}
			left := i.Expire.Sub(now)
			switch {
			case left < 0:
				w.level, w.title, w.body = 3, "Подписка "+name+" закончилась", "Продлите её у провайдера: без этого серверы не работают."
			case left < 24*time.Hour:
				day := "завтра"
				if e, nl := i.Expire.Local(), now.Local(); e.Year() == nl.Year() && e.YearDay() == nl.YearDay() {
					day = "сегодня"
				}
				w.level, w.title, w.body = 2, "Подписка "+name+" закончится "+day, "Продлите её у провайдера, чтобы VPN не отключился."
			case left < 72*time.Hour:
				days := int(math.Ceil(left.Minutes() / (24 * 60)))
				word := "дня"
				if days == 1 {
					word = "день"
				}
				w.level, w.title, w.body = 1, fmt.Sprintf("Подписка %s закончится через %d %s", name, days, word), "Продлите её у провайдера заранее."
			}
			if w.level > 0 {
				out = append(out, w)
			}
		}
		if i.Total > 0 {
			used := i.Upload + i.Download
			w := subWarning{key: s.ID + "/traffic", term: strconv.FormatUint(i.Total, 10)}
			switch {
			case used >= i.Total:
				w.level, w.title, w.body = 3, "Трафик подписки "+name+" закончился", "Докупите трафик у провайдера или дождитесь его обновления."
			case float64(used)/float64(i.Total) >= .9:
				w.level, w.title = 1, "Трафик подписки "+name+" почти израсходован"
				w.body = "Осталось " + formatQuota(float64(i.Total-used)) + " из " + formatQuota(float64(i.Total)) + "."
			}
			if w.level > 0 {
				out = append(out, w)
			}
		}
	}
	return out
}

// formatQuota writes a panel's traffic figure as the app does: a gigabyte
// is 1024³ bytes, so a "100 GB" plan reads «100 ГБ».
func formatQuota(b float64) string {
	const k = 1024.0
	n := func(v float64) string {
		prec := 1
		if v >= 100 {
			prec = 0
		}
		return strings.TrimSuffix(strconv.FormatFloat(v, 'f', prec, 64), ".0")
	}
	switch {
	case b >= k*k*k*k:
		return n(b/(k*k*k*k)) + " ТБ"
	case b >= k*k*k:
		return n(b/(k*k*k)) + " ГБ"
	case b >= k*k:
		return strconv.FormatFloat(b/(k*k), 'f', 0, 64) + " МБ"
	}
	return strconv.FormatFloat(b/k, 'f', 0, 64) + " КБ"
}

// subAlerts remembers, in a file beside the settings, the step each warning
// was notified at, so each step is notified once.
type subAlerts struct {
	path string
}

// due returns the warnings that got worse since they were last notified,
// and remembers them; what no longer applies (renewed, removed) is
// forgotten.
func (a subAlerts) due(subs []store.Subscription, now time.Time) []subWarning {
	warned := map[string]string{}
	if b, err := os.ReadFile(a.path); err == nil {
		json.Unmarshal(b, &warned)
	}
	next := map[string]string{}
	var out []subWarning
	for _, w := range subWarnings(subs, now) {
		before := 0
		if lvl, term, ok := strings.Cut(warned[w.key], "|"); ok && term == w.term {
			before, _ = strconv.Atoi(lvl)
		}
		next[w.key] = strconv.Itoa(max(before, w.level)) + "|" + w.term
		if w.level > before {
			out = append(out, w)
		}
	}
	if len(out) > 0 || len(next) != len(warned) {
		if b, err := json.Marshal(next); err == nil {
			os.WriteFile(a.path, b, 0o600)
		}
	}
	return out
}
