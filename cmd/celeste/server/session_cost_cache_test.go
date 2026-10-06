package server

import (
	"math"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
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

// #312 review: an MCP `celeste agent` turn that reads the prompt cache
// costs less than the same turn without cache reads.
func TestAgentTurnSessionCostPricesTheCache(t *testing.T) {
	var cached, plain sessionCost
	agentTurnRecorder(&cached, "claude-sonnet-4-6")(agent.TurnStats{InputTokens: 25_000, OutputTokens: 500, CacheReadTokens: 20_000})
	agentTurnRecorder(&plain, "claude-sonnet-4-6")(agent.TurnStats{InputTokens: 25_000, OutputTokens: 500})
	if !(cached.usd > 0 && cached.usd < plain.usd) {
		t.Fatalf("cached = %v, plain = %v; want 0 < cached < plain", cached.usd, plain.usd)
	}
	want := costs.CostOf("claude-sonnet-4-6", costs.Usage{Input: 25_000, Output: 500, CacheRead: 20_000})
	if math.Abs(cached.usd-want) > 1e-12 {
		t.Fatalf("cached = %v, want %v", cached.usd, want)
	}
}
