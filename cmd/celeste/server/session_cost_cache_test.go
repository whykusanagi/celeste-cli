package server

import (
	"math"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
)

// #312: celeste_status's session cost prices cache reads and writes at
// their own rates.
func TestSessionCostPricesTheCache(t *testing.T) {
	var c sessionCost
	c.record("claude-sonnet-4-6", &llm.TokenUsage{PromptTokens: 1_000, CompletionTokens: 10, CacheReadTokens: 800, CacheWriteTokens: 100, CacheWrite1hTokens: 40})
	want := costs.CostOf("claude-sonnet-4-6", costs.Usage{Input: 1_000, Output: 10, CacheRead: 800, CacheWrite: 100, CacheWrite1h: 40})
	if math.Abs(c.usd-want) > 1e-12 {
		t.Fatalf("usd = %v, want %v", c.usd, want)
	}
}
