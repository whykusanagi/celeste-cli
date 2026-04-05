// Package costs provides token cost tracking and pricing for LLM models.
package costs

// ModelCost holds the per-1M-token pricing for a model.
type ModelCost struct {
	Input  float64 // USD per 1M input tokens
	Output float64 // USD per 1M output tokens
}

// ModelPricing maps model identifiers to their costs.
var ModelPricing = map[string]ModelCost{
	// OpenAI
	"gpt-4o":      {Input: 2.50, Output: 10.00},
	"gpt-4o-mini": {Input: 0.15, Output: 0.60},
	// xAI Grok
	"grok-4-1-fast":                 {Input: 0.20, Output: 0.50},
	"grok-4-1-fast-reasoning":       {Input: 0.20, Output: 0.50},
	"grok-4-1-fast-non-reasoning":   {Input: 0.20, Output: 0.50},
	"grok-4.20-0309-reasoning":      {Input: 2.00, Output: 6.00},
	"grok-4.20-0309-non-reasoning":  {Input: 2.00, Output: 6.00},
	"grok-4.20-multi-agent-0309":    {Input: 2.00, Output: 6.00},
	// Google
	"gemini-2.0-flash": {Input: 0.10, Output: 0.40},
	// Anthropic
	"claude-sonnet-4": {Input: 3.00, Output: 15.00},
	"claude-opus-4":   {Input: 15.00, Output: 75.00},
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
