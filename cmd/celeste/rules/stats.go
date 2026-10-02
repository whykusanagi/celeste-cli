package rules

import (
	"sort"
	"sync"
)

var stats = struct {
	mu       sync.Mutex
	acted    int
	shadowed int
	byRule   map[string]int
}{byRule: map[string]int{}}

// Record counts a fire: acted, or only logged (stream_rules: shadow).
func Record(name string, acted bool) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	if acted {
		stats.acted++
	} else {
		stats.shadowed++
	}
	stats.byRule[name]++
}

// Snapshot is the celeste_status "rules" object (without "mode", which the
// server adds).
func Snapshot() map[string]any {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	names := make([]string, 0, len(stats.byRule))
	for n := range stats.byRule {
		names = append(names, n)
	}
	sort.Strings(names)
	by := make(map[string]any, len(names))
	for _, n := range names {
		by[n] = stats.byRule[n]
	}
	return map[string]any{
		"fires":    stats.acted + stats.shadowed,
		"acted":    stats.acted,
		"shadowed": stats.shadowed,
		"by_rule":  by,
	}
}

// ResetStats clears the counters (tests).
func ResetStats() {
	stats.mu.Lock()
	stats.acted, stats.shadowed, stats.byRule = 0, 0, map[string]int{}
	stats.mu.Unlock()
}
