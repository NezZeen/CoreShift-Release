package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// statsKeepDays is how many days of traffic are kept on disk.
const statsKeepDays = 90

// statsFlushEvery is how often the day's totals are written out while
// connected; the rest of the time they are written when a connection ends.
const statsFlushEvery = 30 * time.Second

// StatsDay is the traffic of one calendar day through the VPN, in bytes.
type StatsDay struct {
	// Date is the local day, "2006-01-02".
	Date string `json:"date"`
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

// Stats is the answer of GET /v1/stats: the last days, oldest first, with
// the days without traffic included as zeros.
type Stats struct {
	Days []StatsDay `json:"days"`
}

// trafficStats keeps the bytes sent and received through the node per day,
// in a file next to the other state. A damaged or missing file starts the
// statistics over; losing them costs nothing but the history.
type trafficStats struct {
	path string

	mu    sync.Mutex
	days  map[string]*StatsDay
	dirty bool
}

func openStats(path string) *trafficStats {
	t := &trafficStats{path: path, days: map[string]*StatsDay{}}
	var saved []StatsDay
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &saved) == nil {
		for _, d := range saved {
			d := d
			t.days[d.Date] = &d
		}
	}
	return t
}

const dayLayout = "2006-01-02"

// add counts bytes for the day of now.
func (t *trafficStats) add(now time.Time, up, down int64) {
	if up <= 0 && down <= 0 {
		return
	}
	key := now.Format(dayLayout)
	t.mu.Lock()
	defer t.mu.Unlock()
	d := t.days[key]
	if d == nil {
		d = &StatsDay{Date: key}
		t.days[key] = d
	}
	d.Up += max(up, 0)
	d.Down += max(down, 0)
	t.dirty = true
}

// report returns the last n days up to and including today.
func (t *trafficStats) report(now time.Time, n int) Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := Stats{Days: make([]StatsDay, 0, n)}
	for i := n - 1; i >= 0; i-- {
		key := now.AddDate(0, 0, -i).Format(dayLayout)
		if d := t.days[key]; d != nil {
			out.Days = append(out.Days, *d)
		} else {
			out.Days = append(out.Days, StatsDay{Date: key})
		}
	}
	return out
}

// flush writes the totals out when they changed, dropping days older than
// statsKeepDays. A file that cannot be written is not worth failing for.
func (t *trafficStats) flush(now time.Time) {
	t.mu.Lock()
	if !t.dirty {
		t.mu.Unlock()
		return
	}
	cutoff := now.AddDate(0, 0, -statsKeepDays).Format(dayLayout)
	list := make([]StatsDay, 0, len(t.days))
	for k, d := range t.days {
		if k < cutoff {
			delete(t.days, k)
			continue
		}
		list = append(list, *d)
	}
	t.dirty = false
	t.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Date < list[j].Date })
	b, err := json.Marshal(list)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return
	}
	tmp := t.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, t.path) != nil {
		os.Remove(tmp)
	}
}
