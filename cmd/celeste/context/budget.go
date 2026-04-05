// Package ctxmgr provides automatic context window management for Celeste CLI.
// The package name is ctxmgr (not context) to avoid collision with the stdlib
// context package. Import path: github.com/whykusanagi/celeste-cli/cmd/celeste/context
package ctxmgr

import (
	"fmt"
	"sync"
)

// ModelLimits maps model names to their context window sizes in tokens.
// Migrated from config/tokens.go.
var ModelLimits = map[string]int{
	// OpenAI (from /v1/models API, 2026-04)
	"gpt-4.1":       1050000,
	"gpt-4.1-mini":  400000,
	"gpt-4.1-nano":  400000,
	"gpt-5":         1050000,
	"gpt-5-mini":    400000,
	"gpt-5-nano":    400000,
	"gpt-5-pro":     1050000,
	"gpt-5-codex":   1050000,
	"gpt-5.1":       1050000,
	"gpt-5.1-codex": 1050000,
	"gpt-5.2":       1050000,
	"gpt-5.2-codex": 1050000,
	"gpt-5.2-pro":   1050000,
	"gpt-5.3-codex": 1050000,
	"gpt-5.4":       1050000,
	"gpt-5.4-mini":  400000,
	"gpt-5.4-nano":  400000,
	"gpt-5.4-pro":   1050000,
	"o1":            200000,
	"o1-pro":        200000,
	"o3":            1050000,
	"o3-mini":       200000,
	"o4-mini":       400000,
	// Anthropic (current models, 2026-04)
	"claude-opus-4-6":   1000000,
	"claude-sonnet-4-6": 1000000,
	"claude-haiku-4-5":  200000,
	// xAI Grok (from /v1/models API)
	"grok-3":                        131072,
	"grok-3-mini":                   131072,
	"grok-4-0709":                   2000000,
	"grok-4-fast-reasoning":         2000000,
	"grok-4-fast-non-reasoning":     2000000,
	"grok-4-1-fast":                 2000000,
	"grok-4-1-fast-reasoning":       2000000,
	"grok-4-1-fast-non-reasoning":   2000000,
	"grok-4.20-0309-reasoning":      2000000,
	"grok-4.20-0309-non-reasoning":  2000000,
	"grok-4.20-multi-agent-0309":    2000000,
	"grok-code-fast-1":              2000000,
	// Venice-unique models (from docs.venice.ai/models/text, 2026-04)
	"venice-uncensored":                    32000,
	"venice-uncensored-role-play":          128000,
	"deepseek-v3.2":                        160000,
	"qwen3-coder-480b-a35b-instruct":       256000,
	"qwen3-coder-480b-a35b-instruct-turbo": 256000,
	"qwen3-235b-a22b-thinking-2507":        128000,
	"kimi-k2-5":                            256000,
	"zai-org-glm-4.7":                      198000,
	"mistral-small-3-2-24b-instruct":       256000,
	"llama-3.3-70b":                        128000,
	"minimax-m25":                           198000,
	// Default
	"default": 8192,
}

// TokenBudget tracks token usage across all components of a conversation.
// It provides fine-grained tracking beyond a simple current/max counter,
// separating system prompt, tool definitions, conversation history, and
// per-turn usage to enable intelligent compaction decisions.
type TokenBudget struct {
	mu sync.RWMutex

	// Capacity
	ModelLimit int // Total context window for the model

	// Fixed allocations (set once at session start, updated rarely)
	SystemPromptTokens   int // Tokens consumed by system prompt
	ToolDefinitionTokens int // Tokens consumed by tool/function schemas

	// Dynamic tracking
	HistoryTokens    int // Tokens consumed by conversation history (all messages)
	LastPromptTokens int // Prompt tokens from last API response
	LastCompTokens   int // Completion tokens from last API response

	// Counters
	TurnCount    int // Number of user-assistant turn pairs completed
	CompactCount int // Number of times compaction has been triggered
}

// NewTokenBudget creates a TokenBudget for the given model.
// systemPromptTokens and toolDefTokens represent the fixed token overhead
// that is sent with every request.
func NewTokenBudget(modelLimit, systemPromptTokens, toolDefTokens int) *TokenBudget {
	if modelLimit <= 0 {
		modelLimit = ModelLimits["default"]
	}
	return &TokenBudget{
		ModelLimit:           modelLimit,
		SystemPromptTokens:   systemPromptTokens,
		ToolDefinitionTokens: toolDefTokens,
	}
}

// NewTokenBudgetForModel creates a TokenBudget by looking up the model name
// in ModelLimits. If the model is not found, the "default" limit is used.
func NewTokenBudgetForModel(model string, systemPromptTokens, toolDefTokens int) *TokenBudget {
	limit := GetModelLimit(model)
	return NewTokenBudget(limit, systemPromptTokens, toolDefTokens)
}

// AddTurn records token usage from an API response and increments the turn counter.
// promptTokens and completionTokens come from the API's usage field.
// If the API does not return usage, callers should pass estimates.
func (tb *TokenBudget) AddTurn(promptTokens, completionTokens int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.LastPromptTokens = promptTokens
	tb.LastCompTokens = completionTokens
	tb.TurnCount++

	// The prompt tokens from the API include system prompt + tool defs + history,
	// so history = prompt - fixed overhead. We use the API value directly because
	// it is more accurate than our estimates.
	overhead := tb.SystemPromptTokens + tb.ToolDefinitionTokens
	historyFromAPI := promptTokens - overhead
	if historyFromAPI < 0 {
		historyFromAPI = 0
	}
	tb.HistoryTokens = historyFromAPI
}

// SetHistoryTokens directly sets the history token count. Use this when
// recalculating after compaction or when API usage data is unavailable.
func (tb *TokenBudget) SetHistoryTokens(tokens int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.HistoryTokens = tokens
}

// TotalUsed returns the total tokens currently consumed across all components.
func (tb *TokenBudget) TotalUsed() int {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	return tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens
}

// Available returns tokens remaining before hitting the model limit.
// This is the space available for the next prompt + completion.
func (tb *TokenBudget) Available() int {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	avail := tb.ModelLimit - (tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens)
	if avail < 0 {
		return 0
	}
	return avail
}

// GetUsagePercent returns usage as a fraction from 0.0 to 1.0.
func (tb *TokenBudget) GetUsagePercent() float64 {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	if tb.ModelLimit == 0 {
		return 0.0
	}
	used := tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens
	return float64(used) / float64(tb.ModelLimit)
}

// ShouldCompactReactive returns true when usage has crossed the reactive
// compaction threshold (80% of model limit). This is checked after every
// API response.
func (tb *TokenBudget) ShouldCompactReactive() bool {
	return tb.GetUsagePercent() >= 0.80
}

// ShouldCompactProactive returns true when the turn count has reached a
// multiple of the given interval AND usage is above 50%. This catches
// slow-growing sessions before they hit the reactive threshold.
func (tb *TokenBudget) ShouldCompactProactive(interval int) bool {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	if interval <= 0 {
		interval = 20
	}
	if tb.TurnCount == 0 || tb.TurnCount%interval != 0 {
		return false
	}
	// Only compact proactively if we are above 50% usage
	used := tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens
	return float64(used)/float64(tb.ModelLimit) >= 0.50
}

// IncrementCompactCount records that a compaction occurred.
func (tb *TokenBudget) IncrementCompactCount() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.CompactCount++
}

// GetWarningLevel returns a severity string based on current usage.
// Returns "ok", "warn" (75%), "caution" (85%), or "critical" (95%).
func (tb *TokenBudget) GetWarningLevel() string {
	pct := tb.GetUsagePercent()
	switch {
	case pct >= 0.95:
		return "critical"
	case pct >= 0.85:
		return "caution"
	case pct >= 0.75:
		return "warn"
	default:
		return "ok"
	}
}

// Summary returns a human-readable one-line summary of the budget state.
func (tb *TokenBudget) Summary() string {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	used := tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens
	return fmt.Sprintf("%s/%s (%.1f%%)",
		FormatTokenCount(used),
		FormatTokenCount(tb.ModelLimit),
		float64(used)/float64(tb.ModelLimit)*100,
	)
}

// --- Helpers migrated from config/tokens.go ---

// GetModelLimit returns the token limit for a model name.
// Falls back to the "default" entry if the model is not found.
func GetModelLimit(model string) int {
	if limit, ok := ModelLimits[model]; ok {
		return limit
	}
	return ModelLimits["default"]
}

// GetModelLimitWithOverride returns the token limit for a model, using the
// config override if it is positive.
func GetModelLimitWithOverride(model string, configOverride int) int {
	if configOverride > 0 {
		return configOverride
	}
	return GetModelLimit(model)
}

// EstimateTokens approximates token count from text length.
// Uses the rough heuristic of 4 characters per token.
func EstimateTokens(text string) int {
	return len(text) / 4
}

// FormatTokenCount formats a token count with K/M suffix for display.
func FormatTokenCount(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	} else if tokens >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}
