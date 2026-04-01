// cmd/celeste/permissions/denial.go
package permissions

import "sync"

const (
	// suggestRuleThreshold is the number of consecutive denials of the same
	// tool before suggesting the user add a permanent deny rule.
	suggestRuleThreshold = 3

	// suggestStrictModeThreshold is the total number of denials across all
	// tools before suggesting the user switch to strict mode.
	suggestStrictModeThreshold = 5
)

// DenialTracker tracks tool execution denials within a session.
// It is safe for concurrent use.
type DenialTracker struct {
	mu     sync.Mutex
	counts map[string]int // toolName -> denial count
	total  int
}

// NewDenialTracker creates a new DenialTracker.
func NewDenialTracker() *DenialTracker {
	return &DenialTracker{
		counts: make(map[string]int),
	}
}

// RecordDenial records a denial for the given tool.
func (dt *DenialTracker) RecordDenial(toolName string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.counts[toolName]++
	dt.total++
}

// GetDenialCount returns the number of denials recorded for a specific tool.
func (dt *DenialTracker) GetDenialCount(toolName string) int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.counts[toolName]
}

// GetTotalDenials returns the total number of denials across all tools.
func (dt *DenialTracker) GetTotalDenials() int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.total
}

// ShouldSuggestRule returns true if the user has denied the given tool
// enough times (3+) that the system should suggest adding a permanent
// deny rule to their config.
func (dt *DenialTracker) ShouldSuggestRule(toolName string) bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.counts[toolName] >= suggestRuleThreshold
}

// ShouldSuggestStrictMode returns true if the total denials across all
// tools have reached the threshold (5+) where suggesting strict mode
// would be appropriate.
func (dt *DenialTracker) ShouldSuggestStrictMode() bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.total >= suggestStrictModeThreshold
}

// Reset clears all denial tracking data.
func (dt *DenialTracker) Reset() {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.counts = make(map[string]int)
	dt.total = 0
}

// ResetTool clears denial tracking data for a specific tool.
func (dt *DenialTracker) ResetTool(toolName string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if count, ok := dt.counts[toolName]; ok {
		dt.total -= count
		delete(dt.counts, toolName)
	}
}
