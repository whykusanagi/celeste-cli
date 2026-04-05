package config

import (
	"fmt"
	"strings"
	"time"
)

// UsageMetrics tracks token usage and cost for a session
type UsageMetrics struct {
	TotalInputTokens  int       `json:"total_input_tokens"`
	TotalOutputTokens int       `json:"total_output_tokens"`
	TotalTokens       int       `json:"total_tokens"`
	EstimatedCost     float64   `json:"estimated_cost"`
	CompactionCount   int       `json:"compaction_count"`
	TruncationCount   int       `json:"truncation_count"`
	MessageCount      int       `json:"message_count"`
	ConversationStart time.Time `json:"conversation_start"`
	ConversationEnd   time.Time `json:"conversation_end"`
}

// PricingTier represents the cost per million tokens for input and output
type PricingTier struct {
	InputCostPerMillion  float64
	OutputCostPerMillion float64
}

// ModelPricing is the legacy pricing table for backwards compatibility.
// The canonical pricing source is costs/pricing.go (costs.ModelPricing).
// This table is used by the stats dashboard for historical cost estimates.
var ModelPricing = map[string]PricingTier{
	// See costs/pricing.go for the full, current pricing table.
	// This legacy table only needs entries for models that appear in
	// existing session history for cost display purposes.
	"gpt-4.1-nano":      {0.15, 0.60},
	"grok-4-1-fast":     {0.20, 0.50},
	"claude-sonnet-4":   {3.00, 15.00},
	"gemini-2.0-flash":  {0.10, 0.40},
	"venice-uncensored": {0.20, 0.90},
}

// NewUsageMetrics creates a new usage metrics instance
func NewUsageMetrics() *UsageMetrics {
	return &UsageMetrics{
		ConversationStart: time.Now(),
	}
}

// Update updates the usage metrics with new token counts
func (um *UsageMetrics) Update(inputTokens, outputTokens int, model string) {
	um.TotalInputTokens += inputTokens
	um.TotalOutputTokens += outputTokens
	um.TotalTokens = um.TotalInputTokens + um.TotalOutputTokens
	um.ConversationEnd = time.Now()

	// Recalculate cost
	um.EstimatedCost = CalculateCost(model, um.TotalInputTokens, um.TotalOutputTokens)
}

// IncrementMessageCount increments the message counter
func (um *UsageMetrics) IncrementMessageCount() {
	um.MessageCount++
}

// GetDuration returns the duration of the conversation
func (um *UsageMetrics) GetDuration() time.Duration {
	if um.ConversationEnd.IsZero() {
		return time.Since(um.ConversationStart)
	}
	return um.ConversationEnd.Sub(um.ConversationStart)
}

// GetAverageTokensPerMessage returns the average tokens per message
func (um *UsageMetrics) GetAverageTokensPerMessage() float64 {
	if um.MessageCount == 0 {
		return 0
	}
	return float64(um.TotalTokens) / float64(um.MessageCount)
}

// CalculateCost calculates the estimated cost based on token usage and model pricing
func CalculateCost(model string, inputTokens, outputTokens int) float64 {
	// Normalize model name (remove version suffixes, etc.)
	normalizedModel := normalizeModelName(model)

	pricing, ok := ModelPricing[normalizedModel]
	if !ok {
		// Try exact match first
		pricing, ok = ModelPricing[model]
		if !ok {
			// Unknown model, return 0
			return 0.0
		}
	}

	inputCost := (float64(inputTokens) / 1_000_000) * pricing.InputCostPerMillion
	outputCost := (float64(outputTokens) / 1_000_000) * pricing.OutputCostPerMillion

	return inputCost + outputCost
}

// GetModelPricing returns the pricing tier for a model, if available
func GetModelPricing(model string) (PricingTier, bool) {
	normalizedModel := normalizeModelName(model)

	pricing, ok := ModelPricing[normalizedModel]
	if ok {
		return pricing, true
	}

	// Try exact match
	pricing, ok = ModelPricing[model]
	return pricing, ok
}

// normalizeModelName normalizes model names for pricing lookup
func normalizeModelName(model string) string {
	model = strings.ToLower(model)

	// No legacy mapping — return the model name as-is.
	// If it's in the pricing table, cost is calculated.
	// If not, returns 0 cost. Users should use current models.

	// Venice shorthand
	if strings.Contains(model, "venice") {
		return "venice-uncensored"
	}
	if strings.Contains(model, "llama-3.3-70b") || strings.Contains(model, "llama-3-70b") {
		return "llama-3.3-70b"
	}

	return model
}

// FormatCost formats a cost value as a currency string
func FormatCost(cost float64) string {
	if cost < 0.001 {
		return "$0.000"
	}
	if cost < 1.0 {
		return fmt.Sprintf("$%.3f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// FormatNumber formats a number with thousand separators
func FormatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	// Add comma separators
	s := fmt.Sprintf("%d", n)
	result := ""
	for i, digit := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(digit)
	}
	return result
}

// GetCostPerMessage returns the average cost per message
func (um *UsageMetrics) GetCostPerMessage() float64 {
	if um.MessageCount == 0 {
		return 0.0
	}
	return um.EstimatedCost / float64(um.MessageCount)
}
