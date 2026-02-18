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
		{"gpt-4", 8192},
		{"gpt-4-turbo", 128000},
		{"claude-3-opus", 200000},
		{"venice-uncensored", 8192},
		{"unknown-model", 8192}, // Should default
	}

	for _, tt := range tests {
		result := GetModelLimit(tt.model)
		if result != tt.expected {
			t.Errorf("GetModelLimit(%q) = %d, want %d", tt.model, result, tt.expected)
		}
	}
}

func TestTruncateToLimit(t *testing.T) {
	// Create messages that exceed 8K token limit
	// Each message is ~5000 chars = ~1250 tokens + 4 overhead = ~1254 tokens each
	messages := []SessionMessage{
		{Role: "user", Content: strings.Repeat("a", 5000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("b", 5000), Timestamp: time.Now()},
		{Role: "user", Content: strings.Repeat("c", 5000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("d", 5000), Timestamp: time.Now()},
		{Role: "user", Content: strings.Repeat("e", 5000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("f", 5000), Timestamp: time.Now()},
		{Role: "user", Content: strings.Repeat("g", 5000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("h", 5000), Timestamp: time.Now()},
	}
	// Total: 8 messages * ~1254 tokens = ~10,032 tokens (exceeds 8K limit)

	// With 8K limit (85% = 6963 available) and 100 token system prompt (6863 available)
	// Should keep ~5 messages (5 * 1254 = 6270 tokens)
	truncated := TruncateToLimit(messages, "gpt-4", 100)

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
