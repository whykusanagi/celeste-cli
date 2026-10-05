package clock

import (
	"sync"
	"testing"
)

func TestNowStrictlyIncreases(t *testing.T) {
	prev := Now()
	for i := 0; i < 10000; i++ {
		next := Now()
		if !next.After(prev) {
			t.Fatalf("stamp %d = %v, not after %v", i, next, prev)
		}
		// The wall time too: IDs and saved stamps carry no monotonic
		// reading, and macOS's wall clock is coarser than its monotonic one.
		if next.UnixNano() <= prev.UnixNano() {
			t.Fatalf("stamp %d wall time %d, not after %d", i, next.UnixNano(), prev.UnixNano())
		}
		prev = next
	}
}

// Concurrent callers never get the same time: session IDs and message
// stamps both come from it.
func TestNowNeverRepeatsAcrossGoroutines(t *testing.T) {
	const workers, each = 8, 2000
	var mu sync.Mutex
	seen := make(map[int64]bool, workers*each)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]int64, 0, each)
			for range each {
				local = append(local, Now().UnixNano())
			}
			mu.Lock()
			defer mu.Unlock()
			for _, n := range local {
				if seen[n] {
					t.Errorf("time %d handed out twice", n)
				}
				seen[n] = true
			}
		}()
	}
	wg.Wait()
}
