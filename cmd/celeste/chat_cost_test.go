package main

import (
	"math"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// smokeUsage is a cached Anthropic turn: 23,360 prompt tokens read from
// the cache, 72 written, 12 uncached.
var smokeUsage = llm.TokenUsage{PromptTokens: 23_444, CompletionTokens: 300, TotalTokens: 23_744, CacheReadTokens: 23_360, CacheWriteTokens: 72}

// #312: the session cost (the "Session cost" log line and /costs) prices
// cache reads and writes at their own rates.
func TestChatSessionCostPricesTheCache(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	a := deps.adapter
	a.costTracker = costs.NewSessionTracker()
	u := smokeUsage
	a.recordUsage("claude-sonnet-4-6", &u)
	s := a.costTracker.GetSummary()
	want := costs.CostOf("claude-sonnet-4-6", costs.Usage{Input: 23_444, Output: 300, CacheRead: 23_360, CacheWrite: 72})
	if math.Abs(s.TotalCostUSD-want) > 1e-12 {
		t.Fatalf("session cost = %v, want %v", s.TotalCostUSD, want)
	}
	if naive := costs.GetCost("claude-sonnet-4-6", 23_444, 300); s.TotalCostUSD > naive/2 {
		t.Fatalf("session cost %v is not cache-priced (all-input price %v)", s.TotalCostUSD, naive)
	}
	if s.TotalCacheRead != 23_360 || s.TotalCacheWrite != 72 {
		t.Fatalf("cache totals = %d/%d", s.TotalCacheRead, s.TotalCacheWrite)
	}

	got := a.SessionCost()
	if got.CacheRead != 23_360 || got.CacheWrite != 72 || got.Input != 23_444 || got.Output != 300 || got.Requests != 1 || got.USD != s.TotalCostUSD {
		t.Fatalf("SessionCost() = %+v", got)
	}
}

// The usage log line shows the cache reads and writes when there are any.
func TestUsageLogLineShowsTheCache(t *testing.T) {
	u := smokeUsage
	if got, want := usageLine(&u), "Usage: 23444 prompt (cache read 23360, cache write 72) + 300 completion = 23744 tokens"; got != want {
		t.Fatalf("usageLine = %q, want %q", got, want)
	}
	plain := llm.TokenUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}
	if got, want := usageLine(&plain), "Usage: 10 prompt + 2 completion = 12 tokens"; got != want {
		t.Fatalf("usageLine = %q, want %q", got, want)
	}
}

var _ tui.SessionCoster = (*TUIClientAdapter)(nil)
