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
	// Anthropic (current models, 2026-04)
	"claude-opus-4-6":   {Input: 5.00, Output: 25.00},
	"claude-sonnet-4-6": {Input: 3.00, Output: 15.00},
	"claude-haiku-4-5":  {Input: 1.00, Output: 5.00},
	// Venice-unique models (from docs.venice.ai, 2026-04)
	"venice-uncensored":                    {Input: 0.20, Output: 0.90},
	"venice-uncensored-role-play":          {Input: 0.50, Output: 2.00},
	"deepseek-v3.2":                        {Input: 0.33, Output: 0.48},
	"qwen3-coder-480b-a35b-instruct":       {Input: 0.75, Output: 3.00},
	"qwen3-coder-480b-a35b-instruct-turbo": {Input: 0.35, Output: 1.50},
	"qwen3-235b-a22b-thinking-2507":        {Input: 0.45, Output: 3.50},
	"kimi-k2-5":                            {Input: 0.56, Output: 3.50},
	"zai-org-glm-4.7":                      {Input: 0.55, Output: 2.65},
	"mistral-small-3-2-24b-instruct":       {Input: 0.09, Output: 0.25},
	"llama-3.3-70b":                        {Input: 0.70, Output: 2.80},
	"minimax-m25":                           {Input: 0.34, Output: 1.19},
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
