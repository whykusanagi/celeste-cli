package agent

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
)

// TestCapToolCalls guards the fix for the Sakana Fugu 400: a turn must never
// declare more tool_calls than the results it returns. The cap keeps the two in
// lockstep when a model emits more parallel calls than MaxToolCallsPerTurn.
func TestCapToolCalls(t *testing.T) {
	calls := make([]llm.ToolCallResult, 10)
	for i := range calls {
		calls[i] = llm.ToolCallResult{ID: string(rune('a' + i))}
	}

	if got := capToolCalls(calls, 8); len(got) != 8 {
		t.Fatalf("expected 8 calls after cap, got %d", len(got))
	}
	if got := capToolCalls(calls, 0); len(got) != 10 {
		t.Fatalf("maxCalls<=0 means no limit, got %d", len(got))
	}
	if got := capToolCalls(calls[:3], 8); len(got) != 3 {
		t.Fatalf("under the cap should be unchanged, got %d", len(got))
	}
}

func TestConsecutiveInvalidToolArgsCap(t *testing.T) {
	n := 0
	for turn := 0; turn < 3; turn++ {
		n = nextConsecutiveInvalid(n, true)
	}
	if n != 3 {
		t.Fatalf("expected exactly 3 after 3 invalid turns, got %d", n)
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
