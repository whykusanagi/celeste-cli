// Package costs provides token cost tracking and pricing for LLM models.
package costs

// ModelCost holds the per-1M-token pricing for a model.
type ModelCost struct {
	Input  float64 // USD per 1M input tokens
	Output float64 // USD per 1M output tokens
}

// ModelPricing maps model identifiers to their costs.
var ModelPricing = map[string]ModelCost{
	// OpenAI (from pricing page + /v1/models API, 2026-04)
	"gpt-4.1":       {Input: 2.50, Output: 15.00},
	"gpt-4.1-mini":  {Input: 0.75, Output: 4.50},
	"gpt-4.1-nano":  {Input: 0.20, Output: 1.25},
	"gpt-5":         {Input: 2.50, Output: 15.00},
	"gpt-5-mini":    {Input: 0.75, Output: 4.50},
	"gpt-5-nano":    {Input: 0.20, Output: 1.25},
	"gpt-5-pro":     {Input: 15.00, Output: 60.00},
	"gpt-5-codex":   {Input: 2.50, Output: 15.00},
	"gpt-5.1":       {Input: 2.50, Output: 15.00},
	"gpt-5.1-codex": {Input: 2.50, Output: 15.00},
	"gpt-5.2":       {Input: 2.50, Output: 15.00},
	"gpt-5.2-codex": {Input: 2.50, Output: 15.00},
	"gpt-5.2-pro":   {Input: 15.00, Output: 60.00},
	"gpt-5.3-codex": {Input: 2.50, Output: 15.00},
	"gpt-5.4":       {Input: 2.50, Output: 15.00},
	"gpt-5.4-mini":  {Input: 0.75, Output: 4.50},
	"gpt-5.4-nano":  {Input: 0.20, Output: 1.25},
	"gpt-5.4-pro":   {Input: 15.00, Output: 60.00},
	"o1":            {Input: 15.00, Output: 60.00},
	"o1-pro":        {Input: 150.00, Output: 600.00},
	"o3":            {Input: 2.50, Output: 15.00},
	"o3-mini":       {Input: 1.10, Output: 4.40},
	"o4-mini":       {Input: 0.75, Output: 4.50},
	// xAI Grok (from pricing page)
	"grok-4-1-fast":                 {Input: 0.20, Output: 0.50},
	"grok-4-1-fast-reasoning":       {Input: 0.20, Output: 0.50},
	"grok-4-1-fast-non-reasoning":   {Input: 0.20, Output: 0.50},
	"grok-4-fast-reasoning":         {Input: 0.20, Output: 0.50},
	"grok-4-fast-non-reasoning":     {Input: 0.20, Output: 0.50},
	"grok-4.20-0309-reasoning":      {Input: 2.00, Output: 6.00},
	"grok-4.20-0309-non-reasoning":  {Input: 2.00, Output: 6.00},
	"grok-4.20-multi-agent-0309":    {Input: 2.00, Output: 6.00},
	"grok-code-fast-1":              {Input: 0.20, Output: 0.50},
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
