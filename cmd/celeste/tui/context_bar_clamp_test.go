package tui

import (
	"math"
	"testing"
)

// A saved session with a negative token_count, or a usage far past the
// window, reaches the bar as a negative or huge percentage. View must render
// a 10-segment bar either way instead of panicking in strings.Repeat.
func TestContextBarViewNeverPanicsOnOutOfRangeUsage(t *testing.T) {
	cases := []struct {
		name    string
		used    int
		percent float64
	}{
		{"negative tokens", -5000, -10},
		{"very negative", math.MinInt32, -1e12},
		{"huge", math.MaxInt32, 1e12},
		{"nan", 0, math.NaN()},
		{"inf", 0, math.Inf(1)},
		{"neg inf", 0, math.Inf(-1)},
	}
	for _, tc := range cases {
		for _, width := range []int{0, 40, 120} {
			m := NewContextBarModel()
			m.SetSize(width, 1)
			m, _ = m.Update(ContextBudgetMsg{UsedTokens: tc.used, MaxTokens: 128000, UsagePercent: tc.percent, TurnCount: 1})
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s width %d: View panicked: %v", tc.name, width, r)
					}
				}()
				if got := m.View(); got == "" {
					t.Fatalf("%s width %d: empty view", tc.name, width)
				}
			}()
		}
	}
}
