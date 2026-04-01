package config

import (
	"fmt"

	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
)

// ContextTracker monitors token usage and context window status for a session.
// It is a thin wrapper around ctxmgr.TokenBudget, keeping the same external
// API so that tui/app.go and commands/ callers do not need wholesale changes.
type ContextTracker struct {
	Session          *Session
	Model            string
	MaxTokens        int
	CurrentTokens    int
	PromptTokens     int
	CompletionTokens int

	// Thresholds (kept for backward compatibility)
	WarnThreshold     float64 // 0.75
	CautionThreshold  float64 // 0.85
	CriticalThreshold float64 // 0.95

	// Tracking
	LastWarningLevel string
	CompactionCount  int
	TruncationCount  int

	// Underlying budget (exported so callers can use it directly if needed)
	Budget *ctxmgr.TokenBudget
}

// NewContextTracker creates a new context tracker for a session.
// It initialises an internal ctxmgr.TokenBudget.
func NewContextTracker(session *Session, model string, contextLimitOverride ...int) *ContextTracker {
	var maxTokens int
	if len(contextLimitOverride) > 0 && contextLimitOverride[0] > 0 {
		maxTokens = contextLimitOverride[0]
	} else {
		maxTokens = ctxmgr.GetModelLimit(model)
	}

	// Calculate token breakdown from message history
	promptTokens, completionTokens, totalTokens := EstimateSessionTokensByRole(session)

	// Use session's TokenCount if it's higher (from API tracking)
	currentTokens := session.TokenCount
	if currentTokens == 0 {
		currentTokens = totalTokens
	}

	// Create the underlying budget.
	// We don't have separate system-prompt / tool-def counts here, so they
	// are folded into history for now.
	budget := ctxmgr.NewTokenBudget(maxTokens, 0, 0)
	budget.SetHistoryTokens(currentTokens)

	return &ContextTracker{
		Session:           session,
		Model:             model,
		MaxTokens:         maxTokens,
		CurrentTokens:     currentTokens,
		PromptTokens:      promptTokens,
		CompletionTokens:  completionTokens,
		WarnThreshold:     0.75,
		CautionThreshold:  0.85,
		CriticalThreshold: 0.95,
		LastWarningLevel:  "ok",
		CompactionCount:   0,
		TruncationCount:   0,
		Budget:            budget,
	}
}

// UpdateTokens updates token counts from API response.
func (ct *ContextTracker) UpdateTokens(prompt, completion, total int) {
	if total > 0 {
		ct.CurrentTokens = total
	}
	if prompt > 0 {
		ct.PromptTokens = prompt
	}
	if completion > 0 {
		ct.CompletionTokens = completion
	}

	// Update session token count
	if ct.Session != nil {
		ct.Session.TokenCount = ct.CurrentTokens
	}

	// Keep budget in sync
	if ct.Budget != nil {
		if prompt > 0 && completion > 0 {
			ct.Budget.AddTurn(prompt, completion)
		} else {
			ct.Budget.SetHistoryTokens(ct.CurrentTokens)
		}
	}
}

// UpdateFromEstimate updates tokens using character-based estimation.
func (ct *ContextTracker) UpdateFromEstimate() {
	if ct.Session != nil {
		estimated := EstimateSessionTokens(ct.Session)
		ct.CurrentTokens = estimated
		ct.Session.TokenCount = estimated
		if ct.Budget != nil {
			ct.Budget.SetHistoryTokens(estimated)
		}
	}
}

// GetUsagePercentage returns the percentage of context window used (0.0 to 1.0).
func (ct *ContextTracker) GetUsagePercentage() float64 {
	if ct.MaxTokens == 0 {
		return 0.0
	}
	return float64(ct.CurrentTokens) / float64(ct.MaxTokens)
}

// GetWarningLevel returns the current warning level based on usage percentage.
// Uses the tracker's own CurrentTokens/MaxTokens rather than delegating to the
// budget, because external code may set CurrentTokens directly.
func (ct *ContextTracker) GetWarningLevel() string {
	usage := ct.GetUsagePercentage()
	if usage >= ct.CriticalThreshold {
		return "critical"
	} else if usage >= ct.CautionThreshold {
		return "caution"
	} else if usage >= ct.WarnThreshold {
		return "warn"
	}
	return "ok"
}

// ShouldWarn returns true if a warning should be displayed.
func (ct *ContextTracker) ShouldWarn() bool {
	currentLevel := ct.GetWarningLevel()
	return currentLevel != "ok" && currentLevel != ct.LastWarningLevel
}

// ShouldCompact returns true if auto-compaction should be triggered.
func (ct *ContextTracker) ShouldCompact() bool {
	return ct.GetUsagePercentage() >= 0.80
}

// GetRemainingTokens returns the number of tokens remaining before limit.
func (ct *ContextTracker) GetRemainingTokens() int {
	remaining := ct.MaxTokens - ct.CurrentTokens
	if remaining < 0 {
		return 0
	}
	return remaining
}

// EstimateMessagesUntilLimit estimates how many messages can be sent before
// hitting the warning threshold.
func (ct *ContextTracker) EstimateMessagesUntilLimit(avgTokensPerMsg int) int {
	if avgTokensPerMsg <= 0 {
		avgTokensPerMsg = 500
	}

	warnThreshold := int(float64(ct.MaxTokens) * ct.WarnThreshold)
	tokensUntilWarn := warnThreshold - ct.CurrentTokens

	if tokensUntilWarn <= 0 {
		return 0
	}

	return tokensUntilWarn / avgTokensPerMsg
}

// GetStatusEmoji returns an emoji representing the current status.
func (ct *ContextTracker) GetStatusEmoji() string {
	level := ct.GetWarningLevel()
	switch level {
	case "critical":
		return "\xf0\x9f\x94\xb4" // red circle
	case "caution":
		return "\xf0\x9f\x9f\xa0" // orange circle
	case "warn":
		return "\xf0\x9f\x9f\xa1" // yellow circle
	default:
		return "\xf0\x9f\x9f\xa2" // green circle
	}
}

// GetWarningMessage returns a user-friendly warning message.
func (ct *ContextTracker) GetWarningMessage() string {
	level := ct.GetWarningLevel()
	percentage := int(ct.GetUsagePercentage() * 100)

	switch level {
	case "critical":
		return fmt.Sprintf("\xf0\x9f\x9a\xa8 Context at %d%% - will auto-compact on next message", percentage)
	case "caution":
		return fmt.Sprintf("\xe2\x9a\xa0\xef\xb8\x8f  Context at %d%% - compaction recommended", percentage)
	case "warn":
		return fmt.Sprintf("\xe2\x9a\xa0\xef\xb8\x8f  Context at %d%% - consider compaction soon", percentage)
	default:
		return ""
	}
}

// GetContextSummary returns a formatted summary of context usage.
func (ct *ContextTracker) GetContextSummary() string {
	current := ctxmgr.FormatTokenCount(ct.CurrentTokens)
	max := ctxmgr.FormatTokenCount(ct.MaxTokens)
	percentage := ct.GetUsagePercentage() * 100

	return fmt.Sprintf("%s/%s (%.1f%%)", current, max, percentage)
}

// MarkWarningShown updates the last warning level after displaying a warning.
func (ct *ContextTracker) MarkWarningShown() {
	ct.LastWarningLevel = ct.GetWarningLevel()
}

// IncrementCompactionCount increments the compaction counter.
func (ct *ContextTracker) IncrementCompactionCount() {
	ct.CompactionCount++
	if ct.Budget != nil {
		ct.Budget.IncrementCompactCount()
	}
}

// IncrementTruncationCount increments the truncation counter.
func (ct *ContextTracker) IncrementTruncationCount() {
	ct.TruncationCount++
}
