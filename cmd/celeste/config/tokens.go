// Package config provides configuration management for Celeste CLI.
// This file handles token estimation and context window management.
package config

// Model token limits (approximate context windows)
var ModelLimits = map[string]int{
	"gpt-4":             8192,
	"gpt-4-turbo":       128000,
	"gpt-4o":            128000,
	"gpt-4o-mini":       128000, // Standard GPT-4o mini context window
	"gpt-3.5-turbo":     16385,
	"claude-3-opus":     200000,
	"claude-3-sonnet":   200000,
	"claude-3-haiku":    200000,
	"claude-sonnet-4":   200000,
	"claude-opus-4.5":   200000,
	"venice-uncensored": 8192,
	"llama-3.3-70b":     8192,
	"grok-4-1":          128000,
	"grok-4-1-fast":     128000,
	"default":           8192,
}

// EstimateTokens approximates token count (rough: 4 chars = 1 token)
// This is a simple estimation. For production, consider using tiktoken library.
func EstimateTokens(text string) int {
	// Simple estimation: ~4 characters per token
	return len(text) / 4
}

// EstimateMessageTokens counts tokens in a message
func EstimateMessageTokens(msg SessionMessage) int {
	// Role overhead: ~4 tokens
	// Content: estimated
	return 4 + EstimateTokens(msg.Content)
}

// EstimateSessionTokens counts total tokens in session
func EstimateSessionTokens(session *Session) int {
	total := 0
	for _, msg := range session.Messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

// EstimateSessionTokensByRole calculates separate input/output token counts from message history.
// Returns (promptTokens, completionTokens, totalTokens)
// - promptTokens: tokens in user messages + system messages
// - completionTokens: tokens in assistant messages
// This is useful for calculating historical sessions or when API doesn't provide breakdown.
func EstimateSessionTokensByRole(session *Session) (int, int, int) {
	promptTokens := 0
	completionTokens := 0

	for _, msg := range session.Messages {
		msgTokens := EstimateMessageTokens(msg)
		switch msg.Role {
		case "user", "system":
			promptTokens += msgTokens
		case "assistant":
			completionTokens += msgTokens
		}
	}

	return promptTokens, completionTokens, promptTokens + completionTokens
}

// GetModelLimit returns token limit for a model
func GetModelLimit(model string) int {
	if limit, ok := ModelLimits[model]; ok {
		return limit
	}
	return ModelLimits["default"]
}

// GetModelLimitWithOverride returns token limit for a model, with optional config override
func GetModelLimitWithOverride(model string, configOverride int) int {
	// If config has explicit context_limit, use that
	if configOverride > 0 {
		return configOverride
	}
	// Otherwise use model default
	return GetModelLimit(model)
}

// TruncateToLimit removes oldest messages to fit within token limit
func TruncateToLimit(messages []SessionMessage, model string, systemPromptTokens int) []SessionMessage {
	limit := GetModelLimit(model)
	targetLimit := int(float64(limit) * 0.85) // Keep 85% buffer

	// Always keep system prompt overhead
	available := targetLimit - systemPromptTokens

	// Count from newest (end) backwards
	kept := []SessionMessage{}
	cumulative := 0

	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := EstimateMessageTokens(messages[i])
		if cumulative+msgTokens > available {
			break
		}
		cumulative += msgTokens
		kept = append([]SessionMessage{messages[i]}, kept...)
	}

	return kept
}
