package config

import (
	"testing"
	"time"
)

func TestNewUsageMetrics(t *testing.T) {
	metrics := NewUsageMetrics()

	if metrics.TotalTokens != 0 {
		t.Errorf("Expected TotalTokens=0, got %d", metrics.TotalTokens)
	}

	if metrics.ConversationStart.IsZero() {
		t.Error("Expected ConversationStart to be set")
	}
}

func TestUsageMetricsUpdate(t *testing.T) {
	metrics := NewUsageMetrics()

	metrics.Update(1000, 500, "gpt-4o")

	if metrics.TotalInputTokens != 1000 {
		t.Errorf("Expected TotalInputTokens=1000, got %d", metrics.TotalInputTokens)
	}

	if metrics.TotalOutputTokens != 500 {
		t.Errorf("Expected TotalOutputTokens=500, got %d", metrics.TotalOutputTokens)
	}

	if metrics.TotalTokens != 1500 {
		t.Errorf("Expected TotalTokens=1500, got %d", metrics.TotalTokens)
	}

	// Cost for gpt-4o: $2.50/M input, $10.00/M output
	// (1000/1M * 2.50) + (500/1M * 10.00) = 0.0025 + 0.005 = 0.0075
	expectedCost := 0.0075
	if metrics.EstimatedCost != expectedCost {
		t.Errorf("Expected cost=%.4f, got %.4f", expectedCost, metrics.EstimatedCost)
	}

	// Update again
	metrics.Update(500, 250, "gpt-4o")

	if metrics.TotalInputTokens != 1500 {
		t.Errorf("Expected cumulative TotalInputTokens=1500, got %d", metrics.TotalInputTokens)
	}

	if metrics.TotalOutputTokens != 750 {
		t.Errorf("Expected cumulative TotalOutputTokens=750, got %d", metrics.TotalOutputTokens)
	}
}

func TestCalculateCost(t *testing.T) {
	testCases := []struct {
		model       string
		input       int
		output      int
		expectedMin float64
		expectedMax float64
	}{
		{"gpt-4o", 1000, 500, 0.007, 0.008},          // $2.50/M in, $10/M out
		{"gpt-4o-mini", 1000, 500, 0.0004, 0.0005},   // $0.15/M in, $0.60/M out
		{"claude-sonnet-4", 1000, 500, 0.010, 0.011}, // $3.00/M in, $15/M out
		{"grok-4-1-fast", 1000, 500, 0.017, 0.018},   // $5.00/M in, $25/M out
	}

	for _, tc := range testCases {
		cost := CalculateCost(tc.model, tc.input, tc.output)
		if cost < tc.expectedMin || cost > tc.expectedMax {
			t.Errorf("Cost for %s with %d/%d tokens: expected between %.4f-%.4f, got %.4f",
				tc.model, tc.input, tc.output, tc.expectedMin, tc.expectedMax, cost)
		}
	}
}

func TestCalculateCostUnknownModel(t *testing.T) {
	cost := CalculateCost("unknown-model-xyz", 1000, 500)
	if cost != 0.0 {
		t.Errorf("Expected cost=0.0 for unknown model, got %.4f", cost)
	}
}

func TestNormalizeModelName(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"gpt-4o", "gpt-4o"},
		{"gpt-4o-2024-11-20", "gpt-4o"},
		{"GPT-4O", "gpt-4o"},
		{"gpt-4o-mini", "gpt-4o-mini"},
		{"gpt-4-turbo-preview", "gpt-4-turbo"},
		{"claude-3-5-sonnet-20241022", "claude-sonnet-4"},
		{"claude-3-opus-20240229", "claude-opus-4.5"},
		{"claude-3-haiku-20240307", "claude-haiku"},
		{"grok-4-1-fast", "grok-4-1-fast"},
		{"grok-4.1-fast", "grok-4-1-fast"},
		{"gemini-1.5-pro", "gemini-1.5-pro"},
		{"venice-uncensored", "venice-uncensored"},
	}

	for _, tc := range testCases {
		result := normalizeModelName(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeModelName(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
}

func TestGetModelPricing(t *testing.T) {
	testCases := []struct {
		model      string
		shouldFind bool
	}{
		{"gpt-4o", true},
		{"claude-sonnet-4", true},
		{"grok-4-1-fast", true},
		{"unknown-model", false},
	}

	for _, tc := range testCases {
		pricing, found := GetModelPricing(tc.model)
		if found != tc.shouldFind {
			t.Errorf("GetModelPricing(%s): expected found=%v, got %v", tc.model, tc.shouldFind, found)
		}

		if found && pricing.InputCostPerMillion <= 0 {
			t.Errorf("GetModelPricing(%s): expected valid pricing, got %+v", tc.model, pricing)
		}
	}
}

func TestGetDuration(t *testing.T) {
	metrics := NewUsageMetrics()

	// Simulate conversation lasting 1 second
	time.Sleep(100 * time.Millisecond)

	duration := metrics.GetDuration()

	if duration < 100*time.Millisecond {
		t.Errorf("Expected duration >= 100ms, got %v", duration)
	}

	// Set end time explicitly
	metrics.ConversationEnd = metrics.ConversationStart.Add(5 * time.Second)
	duration = metrics.GetDuration()

	if duration != 5*time.Second {
		t.Errorf("Expected duration=5s, got %v", duration)
	}
}

func TestGetAverageTokensPerMessage(t *testing.T) {
	metrics := NewUsageMetrics()

	// No messages
	avg := metrics.GetAverageTokensPerMessage()
	if avg != 0 {
		t.Errorf("Expected avg=0 with no messages, got %f", avg)
	}

	// Add some usage
	metrics.TotalTokens = 3000
	metrics.MessageCount = 10

	avg = metrics.GetAverageTokensPerMessage()
	expected := 300.0

	if avg != expected {
		t.Errorf("Expected avg=%f, got %f", expected, avg)
	}
}

func TestFormatCost(t *testing.T) {
	testCases := []struct {
		cost     float64
		expected string
	}{
		{0.0001, "$0.000"},
		{0.001, "$0.001"},
		{0.0123, "$0.012"},
		{0.123, "$0.123"},
		{1.234, "$1.23"},
		{12.345, "$12.35"},
	}

	for _, tc := range testCases {
		result := FormatCost(tc.cost)
		if result != tc.expected {
			t.Errorf("FormatCost(%.4f) = %s, expected %s", tc.cost, result, tc.expected)
		}
	}
}

func TestGetCostPerMessage(t *testing.T) {
	metrics := NewUsageMetrics()

	// No messages
	costPerMsg := metrics.GetCostPerMessage()
	if costPerMsg != 0 {
		t.Errorf("Expected cost per message=0 with no messages, got %f", costPerMsg)
	}

	// Add usage
	metrics.EstimatedCost = 1.50
	metrics.MessageCount = 10

	costPerMsg = metrics.GetCostPerMessage()
	expected := 0.15

	if costPerMsg != expected {
		t.Errorf("Expected cost per message=%f, got %f", expected, costPerMsg)
	}
}
