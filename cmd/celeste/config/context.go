package config

import (
	"fmt"
	"math"
)

// ContextTracker monitors token usage and context window status for a session
type ContextTracker struct {
	Session          *Session
	Model            string
	MaxTokens        int
	CurrentTokens    int
	PromptTokens     int
	CompletionTokens int

	// Thresholds
	WarnThreshold     float64 // 0.75
	CautionThreshold  float64 // 0.85
	CriticalThreshold float64 // 0.95

	// Tracking
	LastWarningLevel string
	CompactionCount  int
	TruncationCount  int
}

// NewContextTracker creates a new context tracker for a session
func NewContextTracker(session *Session, model string, contextLimitOverride ...int) *ContextTracker {
	// Use override if provided, otherwise use model default
	var maxTokens int
	if len(contextLimitOverride) > 0 && contextLimitOverride[0] > 0 {
		maxTokens = contextLimitOverride[0]
	} else {
		maxTokens = GetModelLimit(model)
	}

	// Calculate token breakdown from message history
	// This provides input/output estimates for sessions without API tracking
	promptTokens, completionTokens, totalTokens := EstimateSessionTokensByRole(session)

	// Use session's TokenCount if it's higher (from API tracking)
	// Otherwise use our estimate
	currentTokens := session.TokenCount
	if currentTokens == 0 {
		currentTokens = totalTokens
	}

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
	}
}

// UpdateTokens updates token counts from API response
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
}

// UpdateFromEstimate updates tokens using character-based estimation
func (ct *ContextTracker) UpdateFromEstimate() {
	if ct.Session != nil {
		estimated := EstimateSessionTokens(ct.Session)
		ct.CurrentTokens = estimated
		ct.Session.TokenCount = estimated
	}
}

// GetUsagePercentage returns the percentage of context window used (0.0 to 1.0)
func (ct *ContextTracker) GetUsagePercentage() float64 {
	if ct.MaxTokens == 0 {
		return 0.0
	}
	return float64(ct.CurrentTokens) / float64(ct.MaxTokens)
}

// GetWarningLevel returns the current warning level based on usage percentage
// Possible values: "ok", "warn", "caution", "critical"
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

// ShouldWarn returns true if a warning should be displayed
func (ct *ContextTracker) ShouldWarn() bool {
	currentLevel := ct.GetWarningLevel()
	// Warn if we've entered a new warning level
	return currentLevel != "ok" && currentLevel != ct.LastWarningLevel
}

// ShouldCompact returns true if auto-compaction should be triggered
func (ct *ContextTracker) ShouldCompact() bool {
	usage := ct.GetUsagePercentage()
	// Trigger compaction at 80% to target 70%
	return usage >= 0.80
}

// GetRemainingTokens returns the number of tokens remaining before limit
func (ct *ContextTracker) GetRemainingTokens() int {
	remaining := ct.MaxTokens - ct.CurrentTokens
	if remaining < 0 {
		return 0
	}
	return remaining
}

// EstimateMessagesUntilLimit estimates how many messages can be sent before hitting limit
func (ct *ContextTracker) EstimateMessagesUntilLimit(avgTokensPerMsg int) int {
	if avgTokensPerMsg <= 0 {
		avgTokensPerMsg = 500 // Default estimate
	}

	warnThreshold := int(float64(ct.MaxTokens) * ct.WarnThreshold)
	tokensUntilWarn := warnThreshold - ct.CurrentTokens

	if tokensUntilWarn <= 0 {
		return 0
	}

	return int(math.Floor(float64(tokensUntilWarn) / float64(avgTokensPerMsg)))
}

// GetStatusEmoji returns an emoji representing the current status
func (ct *ContextTracker) GetStatusEmoji() string {
	level := ct.GetWarningLevel()
	switch level {
	case "critical":
		return "🔴"
	case "caution":
		return "🟠"
	case "warn":
		return "🟡"
	default:
		return "🟢"
	}
}

// GetWarningMessage returns a user-friendly warning message
func (ct *ContextTracker) GetWarningMessage() string {
	level := ct.GetWarningLevel()
	percentage := int(ct.GetUsagePercentage() * 100)

	switch level {
	case "critical":
		return fmt.Sprintf("🚨 Context at %d%% - will auto-compact on next message", percentage)
	case "caution":
		return fmt.Sprintf("⚠️  Context at %d%% - compaction recommended", percentage)
	case "warn":
		return fmt.Sprintf("⚠️  Context at %d%% - consider compaction soon", percentage)
	default:
		return ""
	}
}

// FormatTokenCount formats token count with K/M suffix
func FormatTokenCount(tokens int) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
	} else if tokens >= 1000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

// GetContextSummary returns a formatted summary of context usage
func (ct *ContextTracker) GetContextSummary() string {
	current := FormatTokenCount(ct.CurrentTokens)
	max := FormatTokenCount(ct.MaxTokens)
	percentage := ct.GetUsagePercentage() * 100

	return fmt.Sprintf("%s/%s (%.1f%%)", current, max, percentage)
}

// MarkWarningShown updates the last warning level after displaying a warning
func (ct *ContextTracker) MarkWarningShown() {
	ct.LastWarningLevel = ct.GetWarningLevel()
}

// IncrementCompactionCount increments the compaction counter
func (ct *ContextTracker) IncrementCompactionCount() {
	ct.CompactionCount++
}

// IncrementTruncationCount increments the truncation counter
func (ct *ContextTracker) IncrementTruncationCount() {
	ct.TruncationCount++
}
