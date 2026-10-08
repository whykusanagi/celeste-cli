package costs

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/privfs"
)

// CostSummary is a snapshot of cumulative session costs.
type CostSummary struct {
	Model           string  `json:"model"`
	TotalInput      int     `json:"total_input_tokens"`
	TotalOutput     int     `json:"total_output_tokens"`
	TotalCacheRead  int     `json:"total_cache_read_tokens"`
	TotalCacheWrite int     `json:"total_cache_write_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	Turns           int     `json:"turns"`
	// Requests counts every priced or unpriced request, a dropped reply's too.
	Requests int `json:"requests"`
	// Unpriced counts requests on models missing from ModelPricing: their
	// tokens are totalled but cost nothing, so a zero cost is not read as free.
	Unpriced int `json:"unpriced_requests"`
}

// SessionTracker accumulates token usage and cost across a session.
type SessionTracker struct {
	Model           string  `json:"model"`
	TotalInput      int     `json:"total_input"`
	TotalOutput     int     `json:"total_output"`
	TotalCacheRead  int     `json:"total_cache_read"`
	TotalCacheWrite int     `json:"total_cache_write"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	Turns           int     `json:"turns"`
	Requests        int     `json:"requests"`
	Unpriced        int     `json:"unpriced_requests"`
	mu              sync.Mutex
}

// NewSessionTracker creates a new empty tracker.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{}
}

// RecordUsage adds a turn's token usage and computes the incremental cost.
func (t *SessionTracker) RecordUsage(model string, u Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.record(model, u)
	t.Turns++
}

// RecordCost adds usage and its cost without counting a turn: a reply a
// stream rule dropped was billed, but the person never saw it (2.0 W3).
func (t *SessionTracker) RecordCost(model string, u Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.record(model, u)
}

// record adds usage and its cost; the caller holds t.mu.
func (t *SessionTracker) record(model string, u Usage) {
	t.Model = model
	t.TotalInput += u.Input
	t.TotalOutput += u.Output
	t.TotalCacheRead += u.CacheRead
	t.TotalCacheWrite += u.CacheWrite
	t.Requests++
	if !Priced(model) {
		t.Unpriced++
	}
	t.TotalCostUSD += CostOf(model, u)
}

// GetSummary returns a snapshot of the current session cost state.
func (t *SessionTracker) GetSummary() CostSummary {
	t.mu.Lock()
	defer t.mu.Unlock()

	return CostSummary{
		Model:           t.Model,
		TotalInput:      t.TotalInput,
		TotalOutput:     t.TotalOutput,
		TotalCacheRead:  t.TotalCacheRead,
		TotalCacheWrite: t.TotalCacheWrite,
		TotalCostUSD:    t.TotalCostUSD,
		Turns:           t.Turns,
		Requests:        t.Requests,
		Unpriced:        t.Unpriced,
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
	return atomicfile.Write(path, data, privfs.FilePerm)
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
