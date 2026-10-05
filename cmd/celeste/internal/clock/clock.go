// Package clock is celeste's one process-wide clock that never repeats.
package clock

import (
	"sync"
	"time"
)

var (
	mu   sync.Mutex
	last time.Time
)

// Now is time.Now that never repeats or goes back: a call that would return
// the previous time or earlier gets the previous time plus a microsecond.
// Windows' clock is coarse, so two time.Now calls a few microseconds apart
// can return the same value. The chat orders a turn's tool log against
// message timestamps (loop), and two sessions with one ID overwrite each
// other's file (config.UniqueNanoID); both read this clock.
// ponytail: one process-wide lock; per-caller clocks if it ever shows in a profile.
func Now() time.Time {
	mu.Lock()
	defer mu.Unlock()
	// Round(0) drops the monotonic reading, so the comparison is on the
	// wall time that IDs and saved stamps carry: with it, After compared
	// monotonic readings, and macOS's coarser wall clock could hand out
	// the same UnixNano twice.
	t := time.Now().Round(0)
	if !t.After(last) {
		t = last.Add(time.Microsecond)
	}
	last = t
	return t
}
