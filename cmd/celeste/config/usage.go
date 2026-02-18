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

// ModelPricing contains pricing information for various LLM models (2025 rates)
var ModelPricing = map[string]PricingTier{
	// OpenAI models
	"gpt-4o":            {2.50, 10.00},
	"gpt-4o-mini":       {0.15, 0.60},
	"gpt-4o-2024-11-20": {2.50, 10.00},
	"gpt-4-turbo":       {10.00, 30.00},
	"gpt-4":             {30.00, 60.00},
	"gpt-3.5-turbo":     {0.50, 1.50},
	"gpt-3.5-turbo-16k": {3.00, 4.00},

	// Anthropic Claude models
	"claude-opus-4.5":            {15.00, 75.00},
	"claude-sonnet-4":            {3.00, 15.00},
	"claude-3-5-sonnet-20241022": {3.00, 15.00},
	"claude-3-5-sonnet-20240620": {3.00, 15.00},
	"claude-3-opus-20240229":     {15.00, 75.00},
	"claude-3-sonnet-20240229":   {3.00, 15.00},
	"claude-3-haiku-20240307":    {0.80, 4.00},
	"claude-haiku":               {0.80, 4.00},

	// xAI Grok models
	"grok-4-1-fast": {5.00, 25.00},
	"grok-4-1":      {3.00, 15.00},
	"grok-4":        {3.00, 15.00},
	"grok-3":        {2.00, 10.00},
	"grok-2":        {1.00, 5.00},

	// Google Gemini models
	"gemini-2.0-flash-exp": {0.00, 0.00}, // Free tier
	"gemini-1.5-pro":       {1.25, 5.00},
	"gemini-1.5-flash":     {0.075, 0.30},
	"gemini-1.0-pro":       {0.50, 1.50},

	// Venice.ai models
	"venice-uncensored": {0.00, 0.00}, // Often free/community
	"llama-3.3-70b":     {0.50, 0.50},

	// OpenRouter (approximate, varies by model)
	"meta-llama/llama-3.3-70b-instruct": {0.60, 0.60},
	"anthropic/claude-3-opus":           {15.00, 75.00},
	"anthropic/claude-3-sonnet":         {3.00, 15.00},
	"openai/gpt-4o":                     {2.50, 10.00},
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

	// Handle common patterns
	if strings.Contains(model, "gpt-4o") && !strings.Contains(model, "mini") {
		return "gpt-4o"
	}
	if strings.Contains(model, "gpt-4o-mini") || strings.Contains(model, "gpt-4o mini") {
		return "gpt-4o-mini"
	}
	if strings.Contains(model, "gpt-4-turbo") {
		return "gpt-4-turbo"
	}
	if strings.Contains(model, "gpt-4") && !strings.Contains(model, "turbo") {
		return "gpt-4"
	}
	if strings.Contains(model, "gpt-3.5-turbo-16k") {
		return "gpt-3.5-turbo-16k"
	}
	if strings.Contains(model, "gpt-3.5") {
		return "gpt-3.5-turbo"
	}

	// Claude models
	if strings.Contains(model, "claude-3-5-sonnet") || strings.Contains(model, "claude-sonnet-4") {
		return "claude-sonnet-4"
	}
	if strings.Contains(model, "claude-3-opus") || strings.Contains(model, "claude-opus") {
		return "claude-opus-4.5"
	}
	if strings.Contains(model, "claude-3-haiku") || strings.Contains(model, "claude-haiku") {
		return "claude-haiku"
	}

	// Grok models
	if strings.Contains(model, "grok-4-1-fast") || strings.Contains(model, "grok-4.1-fast") {
		return "grok-4-1-fast"
	}
	if strings.Contains(model, "grok-4-1") || strings.Contains(model, "grok-4.1") {
		return "grok-4-1"
	}
	if strings.Contains(model, "grok-4") {
		return "grok-4"
	}
	if strings.Contains(model, "grok-3") {
		return "grok-3"
	}

	// Gemini models
	if strings.Contains(model, "gemini-2.0-flash") {
		return "gemini-2.0-flash-exp"
	}
	if strings.Contains(model, "gemini-1.5-pro") {
		return "gemini-1.5-pro"
	}
	if strings.Contains(model, "gemini-1.5-flash") {
		return "gemini-1.5-flash"
	}
	if strings.Contains(model, "gemini-1.0") {
		return "gemini-1.0-pro"
	}

	// Venice/Llama
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
