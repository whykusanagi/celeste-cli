package costs

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCost_KnownModel(t *testing.T) {
	// grok-4-1-fast: $0.20/1M input, $0.50/1M output
	cost := GetCost("grok-4-1-fast", 1_000_000, 1_000_000)
	assert.InDelta(t, 0.70, cost, 0.001)
}

func TestGetCost_SmallUsage(t *testing.T) {
	// 1000 input tokens of grok-4-1-fast = 1000/1M * 0.20 = 0.0002
	cost := GetCost("grok-4-1-fast", 1000, 0)
	assert.InDelta(t, 0.0002, cost, 0.00001)
}

func TestGetCost_UnknownModel(t *testing.T) {
	cost := GetCost("unknown-model", 1000, 1000)
	assert.Equal(t, 0.0, cost)
}

func TestGetCost_AllModels(t *testing.T) {
	for model := range ModelPricing {
		cost := GetCost(model, 1000, 1000)
		assert.True(t, cost > 0, "model %s should have non-zero cost", model)
		assert.False(t, math.IsNaN(cost), "model %s cost should not be NaN", model)
	}
}

func TestGetCost_ZeroTokens(t *testing.T) {
	cost := GetCost("grok-4-1-fast", 0, 0)
	assert.Equal(t, 0.0, cost)
}
