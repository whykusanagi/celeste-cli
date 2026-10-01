package config

import (
	"testing"
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
