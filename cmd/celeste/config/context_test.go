package config

import (
	"testing"
	"time"
)

func TestNewContextTracker(t *testing.T) {
	session := &Session{
		ID:         "test-session",
		TokenCount: 1000,
		CreatedAt:  time.Now(),
	}

	tracker := NewContextTracker(session, "grok-4-1-fast")

	if tracker.MaxTokens != 2000000 {
		t.Errorf("Expected MaxTokens=2000000 for grok-4-1-fast, got %d", tracker.MaxTokens)
	}

	if tracker.CurrentTokens != 1000 {
		t.Errorf("Expected CurrentTokens=1000, got %d", tracker.CurrentTokens)
	}

	if tracker.WarnThreshold != 0.75 {
		t.Errorf("Expected WarnThreshold=0.75, got %f", tracker.WarnThreshold)
	}
}

func TestGetUsagePercentage(t *testing.T) {
	session := &Session{TokenCount: 0}
	tracker := NewContextTracker(session, "grok-4-1-fast")

	// Test at various levels (grok-4-1-fast has 2000000 token limit)
	testCases := []struct {
		tokens   int
		expected float64
	}{
		{0, 0.0},
		{1000000, 0.5},  // 50%
		{1500000, 0.75}, // 75%
		{1700000, 0.85}, // 85%
		{2000000, 1.0},  // 100%
	}

	for _, tc := range testCases {
		tracker.CurrentTokens = tc.tokens
		usage := tracker.GetUsagePercentage()
		if usage != tc.expected {
			t.Errorf("At %d tokens, expected usage=%f, got %f", tc.tokens, tc.expected, usage)
		}
	}
}

func TestGetWarningLevel(t *testing.T) {
	session := &Session{TokenCount: 0}
	tracker := NewContextTracker(session, "grok-4-1-fast") // 2M limit

	testCases := []struct {
		tokens int
		level  string
	}{
		{500000, "ok"},         // 25%
		{1500000, "warn"},      // 75%
		{1700000, "caution"},   // 85%
		{1900000, "critical"},  // 95%
	}

	for _, tc := range testCases {
		tracker.CurrentTokens = tc.tokens
		level := tracker.GetWarningLevel()
		if level != tc.level {
			t.Errorf("At %d tokens, expected level=%s, got %s", tc.tokens, tc.level, level)
		}
	}
}

func TestShouldCompact(t *testing.T) {
	session := &Session{TokenCount: 0}
	tracker := NewContextTracker(session, "grok-4-1-fast") // 2M limit

	// Should NOT compact below 80%
	tracker.CurrentTokens = 1560000 // 78%
	if tracker.ShouldCompact() {
		t.Error("Should not compact at 78%")
	}

	// Should compact at 80%
	tracker.CurrentTokens = 1600000 // 80%
	if !tracker.ShouldCompact() {
		t.Error("Should compact at 80%")
	}

	// Should compact above 80%
	tracker.CurrentTokens = 1700000 // 85%
	if !tracker.ShouldCompact() {
		t.Error("Should compact at 85%")
	}
}

func TestGetRemainingTokens(t *testing.T) {
	session := &Session{TokenCount: 50000}
	tracker := NewContextTracker(session, "grok-4-1-fast") // 2M limit
	tracker.CurrentTokens = 50000

	remaining := tracker.GetRemainingTokens()
	expected := 1950000

	if remaining != expected {
		t.Errorf("Expected %d remaining tokens, got %d", expected, remaining)
	}
}

func TestFormatTokenCount(t *testing.T) {
	testCases := []struct {
		tokens   int
		expected string
	}{
		{500, "500"},
		{1500, "1.5K"},
		{128000, "128.0K"},
		{1000000, "1.0M"},
		{2000000, "2.0M"},
	}

	for _, tc := range testCases {
		result := FormatTokenCount(tc.tokens)
		if result != tc.expected {
			t.Errorf("FormatTokenCount(%d) = %s, expected %s", tc.tokens, result, tc.expected)
		}
	}
}

func TestGetContextSummary(t *testing.T) {
	session := &Session{TokenCount: 1000000}
	tracker := NewContextTracker(session, "grok-4-1-fast") // 2M limit
	tracker.CurrentTokens = 1000000

	summary := tracker.GetContextSummary()
	expected := "1.0M/2.0M (50.0%)"

	if summary != expected {
		t.Errorf("Expected summary '%s', got '%s'", expected, summary)
	}
}

func TestUpdateTokens(t *testing.T) {
	session := &Session{TokenCount: 0}
	tracker := NewContextTracker(session, "grok-4-1-fast")

	tracker.UpdateTokens(1000, 500, 1500)

	if tracker.PromptTokens != 1000 {
		t.Errorf("Expected PromptTokens=1000, got %d", tracker.PromptTokens)
	}
	if tracker.CompletionTokens != 500 {
		t.Errorf("Expected CompletionTokens=500, got %d", tracker.CompletionTokens)
	}
	if tracker.CurrentTokens != 1500 {
		t.Errorf("Expected CurrentTokens=1500, got %d", tracker.CurrentTokens)
	}
	if session.TokenCount != 1500 {
		t.Errorf("Expected session.TokenCount=1500, got %d", session.TokenCount)
	}
}
