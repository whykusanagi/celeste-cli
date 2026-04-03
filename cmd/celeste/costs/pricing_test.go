package costs

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCost_KnownModel(t *testing.T) {
	// gpt-4o: $2.50/1M input, $10.00/1M output
	cost := GetCost("gpt-4o", 1_000_000, 1_000_000)
	assert.InDelta(t, 12.50, cost, 0.001)
}

func TestGetCost_SmallUsage(t *testing.T) {
	// 1000 input tokens of gpt-4o = 1000/1M * 2.50 = 0.0025
	cost := GetCost("gpt-4o", 1000, 0)
	assert.InDelta(t, 0.0025, cost, 0.00001)
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
	cost := GetCost("gpt-4o", 0, 0)
	assert.Equal(t, 0.0, cost)
}
