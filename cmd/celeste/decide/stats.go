package decide

import (
	"math"
	"sync"
	"time"
)

// useStats counts one caller's model-backed calls.
type useStats struct {
	calls, answered int
	total, max      time.Duration
}

var stats = struct {
	mu    sync.Mutex
	byUse map[string]*useStats
}{byUse: map[string]*useStats{}}

// Record counts one model-backed call: how long it took and whether it
// answered (false: an error or timeout, so the heuristic answered).
func Record(use string, latency time.Duration, answered bool) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	u := stats.byUse[use]
	if u == nil {
		u = &useStats{}
		stats.byUse[use] = u
	}
	u.calls++
	if answered {
		u.answered++
	}
	u.total += latency
	if latency > u.max {
		u.max = latency
	}
}

// Snapshot is the celeste_status "oracle" object (without "mode", which the
// server adds): totals over every use, then each use.
func Snapshot() map[string]any {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	var all useStats
	byUse := make(map[string]any, len(stats.byUse))
	for name, u := range stats.byUse {
		byUse[name] = render(*u)
		all.calls += u.calls
		all.answered += u.answered
		all.total += u.total
		if u.max > all.max {
			all.max = u.max
		}
	}
	out := render(all)
	out["by_use"] = byUse
	return out
}

func render(u useStats) map[string]any {
	hit, avg := 0.0, 0.0
	if u.calls > 0 {
		hit = math.Round(float64(u.answered)/float64(u.calls)*1000) / 1000
		avg = math.Round(float64(u.total.Milliseconds()) / float64(u.calls))
	}
	return map[string]any{
		"calls":          u.calls,
		"answered":       u.answered,
		"fallbacks":      u.calls - u.answered,
		"hit_rate":       hit,
		"avg_latency_ms": avg,
		"max_latency_ms": u.max.Milliseconds(),
	}
}

// ResetStats clears the counters (tests).
func ResetStats() {
	stats.mu.Lock()
	stats.byUse = map[string]*useStats{}
	stats.mu.Unlock()
}
