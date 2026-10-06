package costs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// #312: Anthropic cache writes are priced at the 5-minute or 1-hour write
// rate and cache reads at the read rate, not at the input rate.
// claude-sonnet-4-6: $3 input, $3.75 5m write, $6 1h write, $0.30 read,
// $15 output per 1M tokens.
func TestCostOfPricesAnthropicCache(t *testing.T) {
	u := Usage{Input: 100_000, CacheRead: 60_000, CacheWrite: 30_000, CacheWrite1h: 10_000, Output: 2_000}
	// uncached 10k*3 + read 60k*0.30 + 5m write 20k*3.75 + 1h write 10k*6 + out 2k*15
	want := 0.030 + 0.018 + 0.075 + 0.060 + 0.030
	assert.InDelta(t, want, CostOf("claude-sonnet-4-6", u), 1e-9)
	assert.Greater(t, GetCost("claude-sonnet-4-6", 100_000, 2_000), CostOf("claude-sonnet-4-6", u), "the reads made it cheaper")
}

// The go/no-go smoke: a cached session priced every prompt token at the
// input rate, about three times the real cost. A turn that reads its
// 23k-token prefix from the cache costs a tenth of that prefix.
func TestCostOfCacheReadIsATenthOnSonnet(t *testing.T) {
	read := CostOf("claude-sonnet-4-6", Usage{Input: 23_360, CacheRead: 23_360})
	full := CostOf("claude-sonnet-4-6", Usage{Input: 23_360})
	assert.InDelta(t, full/10, read, 1e-9)
}

// A model without cache rates prices cached tokens as input (what it
// always did), so no model gets cheaper on a guess.
func TestCostOfWithoutCacheRatesPricesCacheAsInput(t *testing.T) {
	u := Usage{Input: 1_000_000, CacheRead: 400_000, CacheWrite: 100_000, Output: 1_000_000}
	assert.InDelta(t, GetCost("venice-uncensored", 1_000_000, 1_000_000), CostOf("venice-uncensored", u), 1e-9)
}

// Cached tokens a provider reports beyond the prompt never make the
// uncached part negative.
func TestCostOfClampsInconsistentCounts(t *testing.T) {
	u := Usage{Input: 100, CacheRead: 1_000}
	assert.InDelta(t, 1_000.0/1e6*0.30, CostOf("claude-sonnet-4-6", u), 1e-12)
}

func TestCostOfUnknownModel(t *testing.T) {
	assert.Zero(t, CostOf("fugu", Usage{Input: 1000, CacheRead: 500, Output: 10}))
	assert.False(t, Priced("fugu"))
	assert.True(t, Priced("claude-haiku-4-5"))
}

// The tracker totals the cache tokens and prices them (/costs and the
// "Session cost" log line read it).
func TestTrackerTotalsCacheTokens(t *testing.T) {
	tr := NewSessionTracker()
	tr.RecordUsage("claude-sonnet-4-6", Usage{Input: 100_000, CacheRead: 60_000, CacheWrite: 30_000, CacheWrite1h: 10_000, Output: 2_000})
	tr.RecordUsage("fugu", Usage{Input: 10, Output: 1})
	s := tr.GetSummary()
	assert.Equal(t, 100_010, s.TotalInput)
	assert.Equal(t, 60_000, s.TotalCacheRead)
	assert.Equal(t, 30_000, s.TotalCacheWrite)
	assert.Equal(t, 1, s.Unpriced)
	assert.InDelta(t, 0.213, s.TotalCostUSD, 1e-9)
}

// Review: Anthropic's default model is a dated ID
// (claude-sonnet-4-5-20250929) and Bedrock/Vertex spell IDs with a prefix or
// an @date; they price as the model they name, not as unpriced.
func TestDatedAndPrefixedModelIDsArePriced(t *testing.T) {
	u := Usage{Input: 10_000, CacheRead: 8_000, Output: 100}
	want := CostOf("claude-sonnet-4-5", u)
	assert.Greater(t, want, 0.0)
	for _, id := range []string{"claude-sonnet-4-5-20250929", "anthropic.claude-sonnet-4-5", "claude-sonnet-4-5@20250929", "Claude-Sonnet-4-5"} {
		assert.True(t, Priced(id), id)
		assert.InDelta(t, want, CostOf(id, u), 1e-12, id)
	}
	assert.False(t, Priced("claude-sonnet-9-9-20250929"))
}

// The current Claude models are priced, cache reads at each model's rate.
func TestCurrentClaudeModelsArePriced(t *testing.T) {
	for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-1", "claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-sonnet-5"} {
		assert.True(t, Priced(id), id)
	}
	// Claude Opus 5.5: $4 input, $0.20 cache read per 1M.
	assert.InDelta(t, 0.20, CostOf("claude-opus-5-5", Usage{Input: 1_000_000, CacheRead: 1_000_000}), 1e-9)
}
