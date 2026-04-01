// Package config provides configuration management for Celeste CLI.
// This file provides thin wrappers around ctxmgr for token estimation
// and model limit queries. Session-specific helpers that depend on
// config types (SessionMessage, Session) remain here.
package config

import (
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
)

// ModelLimits is kept as an alias for backward compatibility.
// Canonical data lives in ctxmgr.ModelLimits.
var ModelLimits = ctxmgr.ModelLimits

// EstimateTokens approximates token count (delegates to ctxmgr).
func EstimateTokens(text string) int {
	return ctxmgr.EstimateTokens(text)
}

// EstimateMessageTokens counts tokens in a message.
func EstimateMessageTokens(msg SessionMessage) int {
	// Role overhead: ~4 tokens + content
	return 4 + ctxmgr.EstimateTokens(msg.Content)
}

// EstimateSessionTokens counts total tokens in session.
func EstimateSessionTokens(session *Session) int {
	total := 0
	for _, msg := range session.Messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

// EstimateSessionTokensByRole calculates separate input/output token counts.
// Returns (promptTokens, completionTokens, totalTokens).
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

// GetModelLimit returns token limit for a model (delegates to ctxmgr).
func GetModelLimit(model string) int {
	return ctxmgr.GetModelLimit(model)
}

// GetModelLimitWithOverride returns token limit with optional config override
// (delegates to ctxmgr).
func GetModelLimitWithOverride(model string, configOverride int) int {
	return ctxmgr.GetModelLimitWithOverride(model, configOverride)
}

// FormatTokenCount formats token count with K/M suffix (delegates to ctxmgr).
func FormatTokenCount(tokens int) string {
	return ctxmgr.FormatTokenCount(tokens)
}

// TruncateToLimit removes oldest messages to fit within token limit.
// This is a legacy helper used by session.go; new code should use the
// compaction engine from ctxmgr instead.
func TruncateToLimit(messages []SessionMessage, model string, systemPromptTokens int) []SessionMessage {
	limit := GetModelLimit(model)
	targetLimit := int(float64(limit) * 0.85) // Keep 85% buffer

	available := targetLimit - systemPromptTokens

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
