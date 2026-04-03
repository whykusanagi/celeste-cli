// Package costs provides token cost tracking and pricing for LLM models.
package costs

// ModelCost holds the per-1M-token pricing for a model.
type ModelCost struct {
	Input  float64 // USD per 1M input tokens
	Output float64 // USD per 1M output tokens
}

// ModelPricing maps model identifiers to their costs.
var ModelPricing = map[string]ModelCost{
	"gpt-4o":           {Input: 2.50, Output: 10.00},
	"gpt-4o-mini":      {Input: 0.15, Output: 0.60},
	"grok-4-1-fast":    {Input: 3.00, Output: 15.00},
	"gemini-2.0-flash": {Input: 0.10, Output: 0.40},
	"claude-sonnet-4":  {Input: 3.00, Output: 15.00},
	"claude-opus-4":    {Input: 15.00, Output: 75.00},
}

// GetCost calculates the total USD cost for the given token counts.
// Returns 0 if the model is not in the pricing table.
func GetCost(model string, inputTokens, outputTokens int) float64 {
	mc, ok := ModelPricing[model]
	if !ok {
		return 0
	}
	inputCost := float64(inputTokens) / 1_000_000.0 * mc.Input
	outputCost := float64(outputTokens) / 1_000_000.0 * mc.Output
	return inputCost + outputCost
}
