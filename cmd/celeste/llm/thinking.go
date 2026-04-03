// Package llm provides the LLM client for Celeste CLI.
package llm

// ThinkingConfig controls extended thinking / reasoning effort for LLM backends.
// When Enabled is true, compatible backends inject the appropriate parameters
// (e.g. reasoning_effort for OpenAI o-series, thinkingBudget for Gemini).
type ThinkingConfig struct {
	Enabled      bool
	BudgetTokens int    // 0 = provider default
	Level        string // "off", "low", "medium", "high", "max"
}

// LevelToBudget converts a human-friendly level string to a token budget.
// If Level is empty or unrecognised, BudgetTokens is returned as-is (0 = provider default).
func (tc ThinkingConfig) LevelToBudget() int {
	switch tc.Level {
	case "low":
		return 4096
	case "medium":
		return 8192
	case "high":
		return 16384
	case "max":
		return 65536
	default:
		return tc.BudgetTokens
	}
}

// ValidLevels returns the accepted level strings for user-facing validation.
func ValidThinkingLevels() []string {
	return []string{"off", "low", "medium", "high", "max"}
}

// IsValidLevel reports whether level is a recognised thinking level.
func IsValidThinkingLevel(level string) bool {
	for _, l := range ValidThinkingLevels() {
		if l == level {
			return true
		}
	}
	return false
}
