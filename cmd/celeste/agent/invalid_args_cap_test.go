package agent

import "testing"

func TestConsecutiveInvalidToolArgsCap(t *testing.T) {
	n := 0
	for turn := 0; turn < 3; turn++ {
		n = nextConsecutiveInvalid(n, true)
	}
	if n < 3 {
		t.Fatalf("expected >=3 after 3 invalid turns, got %d", n)
	}
	n = nextConsecutiveInvalid(n, false)
	if n != 0 {
		t.Fatalf("expected reset to 0 after a valid turn, got %d", n)
	}
}

func TestDefaultOptionsMaxConsecutiveInvalidToolArgs(t *testing.T) {
	opts := DefaultOptions()
	if opts.MaxConsecutiveInvalidToolArgs != 3 {
		t.Fatalf("expected DefaultOptions().MaxConsecutiveInvalidToolArgs == 3, got %d", opts.MaxConsecutiveInvalidToolArgs)
	}
}
