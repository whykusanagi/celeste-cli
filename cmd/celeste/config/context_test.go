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

	tracker := NewContextTracker(session, "gpt-4o")

	if tracker.MaxTokens != 128000 {
		t.Errorf("Expected MaxTokens=128000 for gpt-4o, got %d", tracker.MaxTokens)
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
	tracker := NewContextTracker(session, "gpt-4o")

	// Test at various levels
	testCases := []struct {
		tokens   int
		expected float64
	}{
		{0, 0.0},
		{64000, 0.5},   // 50%
		{96000, 0.75},  // 75%
		{108800, 0.85}, // 85%
		{128000, 1.0},  // 100%
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
	tracker := NewContextTracker(session, "gpt-4o") // 128K limit

	testCases := []struct {
		tokens int
		level  string
	}{
		{50000, "ok"},        // ~39%
		{96000, "warn"},      // 75%
		{108800, "caution"},  // 85%
		{121600, "critical"}, // 95%
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
	tracker := NewContextTracker(session, "gpt-4o") // 128K limit

	// Should NOT compact below 80%
	tracker.CurrentTokens = 100000 // 78%
	if tracker.ShouldCompact() {
		t.Error("Should not compact at 78%")
	}

	// Should compact at 80%
	tracker.CurrentTokens = 102400 // 80%
	if !tracker.ShouldCompact() {
		t.Error("Should compact at 80%")
	}

	// Should compact above 80%
	tracker.CurrentTokens = 110000 // 85%
	if !tracker.ShouldCompact() {
		t.Error("Should compact at 85%")
	}
}

func TestGetRemainingTokens(t *testing.T) {
	session := &Session{TokenCount: 50000}
	tracker := NewContextTracker(session, "gpt-4o") // 128K limit
	tracker.CurrentTokens = 50000

	remaining := tracker.GetRemainingTokens()
	expected := 78000

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
	session := &Session{TokenCount: 64000}
	tracker := NewContextTracker(session, "gpt-4o") // 128K limit
	tracker.CurrentTokens = 64000

	summary := tracker.GetContextSummary()
	expected := "64.0K/128.0K (50.0%)"

	if summary != expected {
		t.Errorf("Expected summary '%s', got '%s'", expected, summary)
	}
}

func TestUpdateTokens(t *testing.T) {
	session := &Session{TokenCount: 0}
	tracker := NewContextTracker(session, "gpt-4o")

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
