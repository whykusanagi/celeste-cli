package costs

import (
	"encoding/json"
	"os"
	"sync"
)

// CostSummary is a snapshot of cumulative session costs.
type CostSummary struct {
	Model        string  `json:"model"`
	TotalInput   int     `json:"total_input_tokens"`
	TotalOutput  int     `json:"total_output_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Turns        int     `json:"turns"`
}

// SessionTracker accumulates token usage and cost across a session.
type SessionTracker struct {
	Model        string  `json:"model"`
	TotalInput   int     `json:"total_input"`
	TotalOutput  int     `json:"total_output"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Turns        int     `json:"turns"`
	mu           sync.Mutex
}

// NewSessionTracker creates a new empty tracker.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{}
}

// RecordUsage adds a turn's token usage and computes the incremental cost.
func (t *SessionTracker) RecordUsage(model string, inputTokens, outputTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.Model = model
	t.TotalInput += inputTokens
	t.TotalOutput += outputTokens
	t.TotalCostUSD += GetCost(model, inputTokens, outputTokens)
	t.Turns++
}

// GetSummary returns a snapshot of the current session cost state.
func (t *SessionTracker) GetSummary() CostSummary {
	t.mu.Lock()
	defer t.mu.Unlock()

	return CostSummary{
		Model:        t.Model,
		TotalInput:   t.TotalInput,
		TotalOutput:  t.TotalOutput,
		TotalCostUSD: t.TotalCostUSD,
		Turns:        t.Turns,
	}
}

// Save serialises the tracker state to a JSON file.
func (t *SessionTracker) Save(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load deserialises tracker state from a JSON file.
func (t *SessionTracker) Load(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, t)
}
