package loop

import (
	"sync"
	"time"
)

var (
	clockMu   sync.Mutex
	lastStamp time.Time
)

// stampNow is time.Now that never repeats or goes back. The chat orders a
// turn's tool log against message timestamps, and on Windows two time.Now
// calls a few microseconds apart can return the same value, which put a tool
// log after the reply it belongs before.
// ponytail: one process-wide lock; per-loop clocks if it ever shows in a profile.
func stampNow() time.Time {
	clockMu.Lock()
	defer clockMu.Unlock()
	t := time.Now()
	if !t.After(lastStamp) {
		t = lastStamp.Add(time.Microsecond)
	}
	lastStamp = t
	return t
}
