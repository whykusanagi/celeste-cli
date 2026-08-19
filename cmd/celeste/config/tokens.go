// Package config provides configuration management for Celeste CLI.
// This file provides thin wrappers around ctxmgr for token estimation
// and model limit queries. Session-specific helpers that depend on
// config types (SessionMessage, Session) remain here.
package config

import (
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
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

// LookupModelLimit reports the limit and whether the model is known
// (delegates to ctxmgr). Callers validating a user-configured window need the
// second value: for an unknown model the limit is a fallback, not knowledge.
func LookupModelLimit(model string) (int, bool) {
	return ctxmgr.LookupModelLimit(model)
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

// ResolveContextLimit returns the effective context window and whether that
// number is actually knowledge.
//
// An explicit override always wins. Otherwise the model is looked up, EXCEPT
// for local endpoints: a local server names its model whatever it likes, so a
// hit in the limits table is coincidence. That mattered in practice — a fresh
// profile inherits the seed default's model (fugu), so pointing it at a local
// server produced a confident 1,000,000-token budget for a server that might
// have 8k. celeste would never compact and the request would overflow.
func ResolveContextLimit(baseURL, model string, override int) (limit int, known bool) {
	if override > 0 {
		return override, true
	}
	limit, known = LookupModelLimit(model)
	if known && providers.DetectProvider(baseURL) == "local" {
		// Fall back to the conservative default rather than a name collision.
		fallback, _ := LookupModelLimit("")
		return fallback, false
	}
	return limit, known
}
