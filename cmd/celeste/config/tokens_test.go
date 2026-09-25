package config

import (
	"strings"
	"testing"
	"time"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"", 0},
		{"Hell", 1},
		{"Hello wor", 2},
		{"This is a longer test message", 7},
	}

	for _, tt := range tests {
		result := EstimateTokens(tt.text)
		if result != tt.expected {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, result, tt.expected)
		}
	}
}

func TestGetModelLimit(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{"gpt-4.1", 1050000},
		{"gpt-4.1-nano", 400000},
		{"claude-opus-4-6", 1000000},
		{"venice-uncensored", 32000},
		{"unknown-model", 128000}, // Should default (#201)
	}

	for _, tt := range tests {
		result := GetModelLimit(tt.model)
		if result != tt.expected {
			t.Errorf("GetModelLimit(%q) = %d, want %d", tt.model, result, tt.expected)
		}
	}
}

func TestTruncateToLimit(t *testing.T) {
	// Create messages that exceed venice-uncensored's 32K limit
	// Each message is ~20000 chars = ~5000 tokens + 4 overhead = ~5004 tokens each
	messages := []SessionMessage{
		{Role: "user", Content: strings.Repeat("a", 20000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("b", 20000), Timestamp: time.Now()},
		{Role: "user", Content: strings.Repeat("c", 20000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("d", 20000), Timestamp: time.Now()},
		{Role: "user", Content: strings.Repeat("e", 20000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("f", 20000), Timestamp: time.Now()},
		{Role: "user", Content: strings.Repeat("g", 20000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("h", 20000), Timestamp: time.Now()},
	}
	// Total: 8 messages * ~5004 tokens = ~40,032 tokens (exceeds 32K limit)

	// With 32K limit (85% = 27200 available) and 100 token system prompt (27100 available)
	// Should keep ~5 messages (5 * 5004 = 25020 tokens)
	truncated := TruncateToLimit(messages, "venice-uncensored", 100)

	if len(truncated) >= len(messages) {
		t.Errorf("Expected truncation, got %d messages (original %d)", len(truncated), len(messages))
	}

	// Should have kept some messages
	if len(truncated) == 0 {
		t.Error("Should have kept some messages")
	}

	// Should keep newest messages
	if truncated[len(truncated)-1].Content != messages[len(messages)-1].Content {
		t.Error("Should keep newest messages")
	}
}

func TestTruncateToLimitNoTruncation(t *testing.T) {
	// Create messages that fit within limit
	messages := []SessionMessage{
		{Role: "user", Content: "Hello", Timestamp: time.Now()},
		{Role: "assistant", Content: "Hi there", Timestamp: time.Now()},
	}

	truncated := TruncateToLimit(messages, "gpt-4", 100)

	// Should keep all messages
	if len(truncated) != len(messages) {
		t.Errorf("Should not truncate, got %d messages (original %d)", len(truncated), len(messages))
	}
}
