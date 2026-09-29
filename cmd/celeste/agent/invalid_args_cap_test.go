package agent

import (
	"testing"
)

// TestCapToolCalls and TestConsecutiveInvalidToolArgsCap moved to the loop
// (loop.TestLoopCapsCallsPerTurn, loop.TestLoopInvalidArgsGuard) with 2.0 F2a.

func TestDefaultOptionsMaxConsecutiveInvalidToolArgs(t *testing.T) {
	opts := DefaultOptions()
	if opts.MaxConsecutiveInvalidToolArgs != 3 {
		t.Fatalf("expected DefaultOptions().MaxConsecutiveInvalidToolArgs == 3, got %d", opts.MaxConsecutiveInvalidToolArgs)
	}
}
