package ctxmgr

import (
	"testing"
)

func TestNewTokenBudget(t *testing.T) {
	tb := NewTokenBudget(128000, 2000, 500)
	if tb.ModelLimit != 128000 {
		t.Errorf("ModelLimit = %d, want 128000", tb.ModelLimit)
	}
	if tb.SystemPromptTokens != 2000 {
		t.Errorf("SystemPromptTokens = %d, want 2000", tb.SystemPromptTokens)
	}
	if tb.ToolDefinitionTokens != 500 {
		t.Errorf("ToolDefinitionTokens = %d, want 500", tb.ToolDefinitionTokens)
	}
	if tb.TurnCount != 0 {
		t.Errorf("TurnCount = %d, want 0", tb.TurnCount)
	}
}

func TestNewTokenBudget_ZeroLimit(t *testing.T) {
	tb := NewTokenBudget(0, 100, 50)
	if tb.ModelLimit != ModelLimits["default"] {
		t.Errorf("ModelLimit = %d, want default %d", tb.ModelLimit, ModelLimits["default"])
	}
}

func TestNewTokenBudgetForModel(t *testing.T) {
	tb := NewTokenBudgetForModel("gpt-4o", 1000, 500)
	if tb.ModelLimit != 128000 {
		t.Errorf("ModelLimit = %d, want 128000", tb.ModelLimit)
	}

	tb2 := NewTokenBudgetForModel("unknown-model", 100, 50)
	if tb2.ModelLimit != ModelLimits["default"] {
		t.Errorf("ModelLimit for unknown = %d, want default %d", tb2.ModelLimit, ModelLimits["default"])
	}
}

func TestAddTurn(t *testing.T) {
	tb := NewTokenBudget(128000, 2000, 500)
	tb.AddTurn(5000, 1000)

	if tb.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", tb.TurnCount)
	}
	if tb.LastPromptTokens != 5000 {
		t.Errorf("LastPromptTokens = %d, want 5000", tb.LastPromptTokens)
	}
	if tb.LastCompTokens != 1000 {
		t.Errorf("LastCompTokens = %d, want 1000", tb.LastCompTokens)
	}
	// History = prompt - overhead = 5000 - 2500 = 2500
	if tb.HistoryTokens != 2500 {
		t.Errorf("HistoryTokens = %d, want 2500", tb.HistoryTokens)
	}
}

func TestAddTurn_PromptLessThanOverhead(t *testing.T) {
	tb := NewTokenBudget(128000, 5000, 3000)
	// Prompt tokens less than overhead -- should not go negative
	tb.AddTurn(1000, 500)
	if tb.HistoryTokens != 0 {
		t.Errorf("HistoryTokens = %d, want 0 (should not go negative)", tb.HistoryTokens)
	}
}

func TestTotalUsed(t *testing.T) {
	tb := NewTokenBudget(128000, 2000, 500)
	tb.AddTurn(12500, 2000) // history = 12500 - 2500 = 10000
	// Total = 2000 + 500 + 10000 = 12500
	if tb.TotalUsed() != 12500 {
		t.Errorf("TotalUsed = %d, want 12500", tb.TotalUsed())
	}
}

func TestAvailable(t *testing.T) {
	tb := NewTokenBudget(10000, 1000, 500)
	tb.SetHistoryTokens(3000)
	// Available = 10000 - (1000 + 500 + 3000) = 5500
	if tb.Available() != 5500 {
		t.Errorf("Available = %d, want 5500", tb.Available())
	}
}

func TestAvailable_Overbudget(t *testing.T) {
	tb := NewTokenBudget(1000, 500, 300)
	tb.SetHistoryTokens(500)
	// Used = 1300, limit = 1000, available should be 0 (not negative)
	if tb.Available() != 0 {
		t.Errorf("Available = %d, want 0 when overbudget", tb.Available())
	}
}

func TestGetUsagePercent(t *testing.T) {
	tb := NewTokenBudget(10000, 1000, 500)
	tb.SetHistoryTokens(6500)
	// Used = 8000, pct = 0.80
	pct := tb.GetUsagePercent()
	if pct < 0.799 || pct > 0.801 {
		t.Errorf("GetUsagePercent = %f, want ~0.80", pct)
	}
}

func TestGetUsagePercent_ZeroLimit(t *testing.T) {
	tb := &TokenBudget{ModelLimit: 0}
	if tb.GetUsagePercent() != 0.0 {
		t.Errorf("GetUsagePercent with zero limit should be 0.0")
	}
}

func TestShouldCompactReactive(t *testing.T) {
	tb := NewTokenBudget(10000, 1000, 500)

	// 70% -- should NOT trigger
	tb.SetHistoryTokens(5500) // total = 7000
	if tb.ShouldCompactReactive() {
		t.Error("ShouldCompactReactive should be false at 70%")
	}

	// 80% -- SHOULD trigger
	tb.SetHistoryTokens(6500) // total = 8000
	if !tb.ShouldCompactReactive() {
		t.Error("ShouldCompactReactive should be true at 80%")
	}

	// 95% -- SHOULD trigger
	tb.SetHistoryTokens(8000) // total = 9500
	if !tb.ShouldCompactReactive() {
		t.Error("ShouldCompactReactive should be true at 95%")
	}
}

func TestShouldCompactProactive(t *testing.T) {
	tb := NewTokenBudget(10000, 1000, 500)

	// At turn 0 -- never
	if tb.ShouldCompactProactive(20) {
		t.Error("ShouldCompactProactive should be false at turn 0")
	}

	// Simulate 20 turns at 55% usage
	tb.SetHistoryTokens(4000) // total = 5500 = 55%
	tb.mu.Lock()
	tb.TurnCount = 20
	tb.mu.Unlock()

	if !tb.ShouldCompactProactive(20) {
		t.Error("ShouldCompactProactive should be true at turn 20 with 55% usage")
	}

	// At turn 19 -- should NOT trigger
	tb.mu.Lock()
	tb.TurnCount = 19
	tb.mu.Unlock()
	if tb.ShouldCompactProactive(20) {
		t.Error("ShouldCompactProactive should be false at turn 19")
	}

	// At turn 20 but only 30% usage -- should NOT trigger
	tb.mu.Lock()
	tb.TurnCount = 20
	tb.mu.Unlock()
	tb.SetHistoryTokens(1500) // total = 3000 = 30%
	if tb.ShouldCompactProactive(20) {
		t.Error("ShouldCompactProactive should be false at 30% even on interval turn")
	}
}

func TestGetWarningLevel(t *testing.T) {
	tb := NewTokenBudget(10000, 0, 0)

	tests := []struct {
		history int
		want    string
	}{
		{5000, "ok"},
		{7500, "warn"},
		{8500, "caution"},
		{9500, "critical"},
	}
	for _, tt := range tests {
		tb.SetHistoryTokens(tt.history)
		got := tb.GetWarningLevel()
		if got != tt.want {
			t.Errorf("GetWarningLevel at %d tokens = %q, want %q", tt.history, got, tt.want)
		}
	}
}

func TestGetModelLimit(t *testing.T) {
	if GetModelLimit("gpt-4o") != 128000 {
		t.Errorf("gpt-4o limit wrong")
	}
	if GetModelLimit("nonexistent") != ModelLimits["default"] {
		t.Errorf("fallback limit wrong")
	}
}

func TestEstimateTokens(t *testing.T) {
	// 20 characters -> 5 tokens
	if EstimateTokens("12345678901234567890") != 5 {
		t.Errorf("EstimateTokens(20 chars) = %d, want 5", EstimateTokens("12345678901234567890"))
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{500, "500"},
		{1500, "1.5K"},
		{128000, "128.0K"},
		{1500000, "1.5M"},
	}
	for _, tt := range tests {
		got := FormatTokenCount(tt.in)
		if got != tt.want {
			t.Errorf("FormatTokenCount(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSummary(t *testing.T) {
	tb := NewTokenBudget(128000, 2000, 500)
	tb.SetHistoryTokens(10000)
	s := tb.Summary()
	// Should contain formatted numbers and percentage
	if s == "" {
		t.Error("Summary should not be empty")
	}
}
