package loop

import "testing"

func TestStampNowStrictlyIncreases(t *testing.T) {
	prev := stampNow()
	for i := 0; i < 10000; i++ {
		next := stampNow()
		if !next.After(prev) {
			t.Fatalf("stamp %d = %v, not after %v", i, next, prev)
		}
		prev = next
	}
}
