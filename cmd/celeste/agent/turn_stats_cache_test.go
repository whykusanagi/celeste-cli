package agent

import (
	"io"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
)

// #312 review: turn stats carry the cache reads and writes the backend
// recorded, so the MCP session cost can price them at their own rates.
func TestTurnStatsCarryCacheUsage(t *testing.T) {
	usage := &llm.TokenUsage{PromptTokens: 1_000, CompletionTokens: 20, CacheReadTokens: 700, CacheWriteTokens: 100, CacheWrite1hTokens: 40}
	for _, kind := range []loop.EventKind{loop.EventAssistant, loop.EventRuleInterrupt} {
		var got []TurnStats
		opts := DefaultOptions()
		opts.OnTurnStats = func(s TurnStats) { got = append(got, s) }
		r := &Runner{options: opts, out: io.Discard, errOut: io.Discard}
		r.onEvent(&RunState{Options: opts}, 0, loop.Event{Kind: kind, Usage: usage})
		if len(got) != 1 {
			t.Fatalf("kind %v: %d stats", kind, len(got))
		}
		s := got[0]
		if s.InputTokens != 1_000 || s.OutputTokens != 20 || s.CacheReadTokens != 700 || s.CacheWriteTokens != 100 || s.CacheWrite1hTokens != 40 {
			t.Fatalf("kind %v: stats = %+v", kind, s)
		}
	}
}
