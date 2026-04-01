# Plan 3: Context Window Management

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatic context window management with token budgets, reactive/proactive compaction, and tool result capping.
**Architecture:** New `context/` package under `cmd/celeste/` replaces `config/context.go` and `config/tokens.go`. Provides TokenBudget tracking, three compaction strategies, and integration hooks for the TUI and agent runtime.
**Tech Stack:** Go 1.26, standard library
**Prerequisite Plans:** Plan 1 (Unified Tool Layer)

---

## File inventory (current state)

| File | Lines | What it does |
|------|-------|-------------|
| `cmd/celeste/config/context.go` | 219 | `ContextTracker` with `MaxTokens`, `CurrentTokens`, `PromptTokens`, `CompletionTokens`. Methods: `UpdateTokens()`, `GetUsagePercentage()`, `ShouldWarn()` (75%/85%/95% thresholds), `ShouldCompact()` (80%). Warning display but NO auto-compaction. |
| `cmd/celeste/config/tokens.go` | 110 | `ModelLimits` map (20+ models), `EstimateTokens()` (4 chars = 1 token), `GetModelLimit()`, `TruncateToLimit()` |
| `cmd/celeste/llm/summarize.go` | 277 | `Summarizer` with `SummarizeMessages()` (creates 150-250 word summaries via LLM), `CompactSession()` (replaces old messages with summary). Has compaction trigger logic at 80% but it is not wired into the main loop. |
| `cmd/celeste/tui/app.go` | ~2500 | Token usage displayed in status bar from `StreamDoneMsg.Usage`. `contextTracker *config.ContextTracker` field on `AppModel`. |
| `cmd/celeste/tui/messages.go` | ~100 | `ChatMessage` struct, `StreamDoneMsg` with `Usage *TokenUsage`, `TokenUsage` struct. |
| `cmd/celeste/config/session.go` | 559 | `Session` stores messages, `TokenCount`, `UsageMetrics`. `SessionMessage` with Role/Content/Timestamp. |
| `cmd/celeste/agent/runtime.go` | ~300 | `Runner` struct with `client`, `registry`, `options`. Agent loop with tool calling. |

## New files created by this plan

| File | Purpose |
|------|---------|
| `cmd/celeste/context/budget.go` | `TokenBudget` type, model limits map, estimation helpers |
| `cmd/celeste/context/budget_test.go` | Tests for TokenBudget |
| `cmd/celeste/context/limits.go` | Tool result capping with disk spillover |
| `cmd/celeste/context/limits_test.go` | Tests for tool result capping |
| `cmd/celeste/context/compaction.go` | `CompactionEngine` with reactive/proactive/snip strategies |
| `cmd/celeste/context/compaction_test.go` | Tests for CompactionEngine |
| `cmd/celeste/context/summarizer.go` | `Summarizer` interface + LLM-backed implementation |
| `cmd/celeste/context/summarizer_test.go` | Tests for Summarizer |

## Files modified by this plan

| File | Change |
|------|--------|
| `cmd/celeste/tui/app.go` | Replace `contextTracker *config.ContextTracker` with `tokenBudget *context.TokenBudget`, add compaction trigger on `StreamDoneMsg` |
| `cmd/celeste/main.go` | Cap tool results in `TUIClientAdapter` before returning |
| `cmd/celeste/agent/runtime.go` | Track budget per turn, trigger compaction between turns |
| `cmd/celeste/config/context.go` | **DELETE** |
| `cmd/celeste/config/tokens.go` | **DELETE** |
| `cmd/celeste/llm/summarize.go` | **DELETE** (logic migrated to `context/summarizer.go`) |

---

## Task 1: TokenBudget type

**File:** `cmd/celeste/context/budget.go`

- [ ] Create `cmd/celeste/context/` package directory
- [ ] Define `TokenBudget` struct with all fields
- [ ] Migrate `ModelLimits` map from `config/tokens.go`
- [ ] Implement `NewTokenBudget()` constructor
- [ ] Implement `AddTurn()` for updating after each API response
- [ ] Implement `GetUsagePercent()`, `Available()`, `ShouldCompactReactive()`, `ShouldCompactProactive()`
- [ ] Implement `EstimateTokens()` and `FormatTokenCount()` (migrated from config)
- [ ] Write comprehensive tests in `budget_test.go`

### Complete implementation

```go
// cmd/celeste/context/budget.go
package context

import (
	"fmt"
	"sync"
)

// ModelLimits maps model names to their context window sizes in tokens.
// Migrated from config/tokens.go.
var ModelLimits = map[string]int{
	"gpt-4":             8192,
	"gpt-4-turbo":       128000,
	"gpt-4o":            128000,
	"gpt-4o-mini":       128000,
	"gpt-3.5-turbo":     16385,
	"claude-3-opus":     200000,
	"claude-3-sonnet":   200000,
	"claude-3-haiku":    200000,
	"claude-sonnet-4":   200000,
	"claude-opus-4.5":   200000,
	"venice-uncensored": 8192,
	"llama-3.3-70b":     8192,
	"grok-4-1":          128000,
	"grok-4-1-fast":     128000,
	"default":           8192,
}

// TokenBudget tracks token usage across all components of a conversation.
// It provides fine-grained tracking beyond a simple current/max counter,
// separating system prompt, tool definitions, conversation history, and
// per-turn usage to enable intelligent compaction decisions.
type TokenBudget struct {
	mu sync.RWMutex

	// Capacity
	ModelLimit int // Total context window for the model

	// Fixed allocations (set once at session start, updated rarely)
	SystemPromptTokens  int // Tokens consumed by system prompt
	ToolDefinitionTokens int // Tokens consumed by tool/function schemas

	// Dynamic tracking
	HistoryTokens    int // Tokens consumed by conversation history (all messages)
	LastPromptTokens int // Prompt tokens from last API response
	LastCompTokens   int // Completion tokens from last API response

	// Counters
	TurnCount    int // Number of user-assistant turn pairs completed
	CompactCount int // Number of times compaction has been triggered
}

// NewTokenBudget creates a TokenBudget for the given model.
// systemPromptTokens and toolDefTokens represent the fixed token overhead
// that is sent with every request.
func NewTokenBudget(modelLimit, systemPromptTokens, toolDefTokens int) *TokenBudget {
	if modelLimit <= 0 {
		modelLimit = ModelLimits["default"]
	}
	return &TokenBudget{
		ModelLimit:           modelLimit,
		SystemPromptTokens:  systemPromptTokens,
		ToolDefinitionTokens: toolDefTokens,
	}
}

// NewTokenBudgetForModel creates a TokenBudget by looking up the model name
// in ModelLimits. If the model is not found, the "default" limit is used.
func NewTokenBudgetForModel(model string, systemPromptTokens, toolDefTokens int) *TokenBudget {
	limit := GetModelLimit(model)
	return NewTokenBudget(limit, systemPromptTokens, toolDefTokens)
}

// AddTurn records token usage from an API response and increments the turn counter.
// promptTokens and completionTokens come from the API's usage field.
// If the API does not return usage, callers should pass estimates.
func (tb *TokenBudget) AddTurn(promptTokens, completionTokens int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.LastPromptTokens = promptTokens
	tb.LastCompTokens = completionTokens
	tb.TurnCount++

	// The prompt tokens from the API include system prompt + tool defs + history,
	// so history = prompt - fixed overhead. We use the API value directly because
	// it is more accurate than our estimates.
	overhead := tb.SystemPromptTokens + tb.ToolDefinitionTokens
	historyFromAPI := promptTokens - overhead
	if historyFromAPI < 0 {
		historyFromAPI = 0
	}
	tb.HistoryTokens = historyFromAPI
}

// SetHistoryTokens directly sets the history token count. Use this when
// recalculating after compaction or when API usage data is unavailable.
func (tb *TokenBudget) SetHistoryTokens(tokens int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.HistoryTokens = tokens
}

// TotalUsed returns the total tokens currently consumed across all components.
func (tb *TokenBudget) TotalUsed() int {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	return tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens
}

// Available returns tokens remaining before hitting the model limit.
// This is the space available for the next prompt + completion.
func (tb *TokenBudget) Available() int {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	avail := tb.ModelLimit - (tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens)
	if avail < 0 {
		return 0
	}
	return avail
}

// GetUsagePercent returns usage as a fraction from 0.0 to 1.0.
func (tb *TokenBudget) GetUsagePercent() float64 {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	if tb.ModelLimit == 0 {
		return 0.0
	}
	used := tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens
	return float64(used) / float64(tb.ModelLimit)
}

// ShouldCompactReactive returns true when usage has crossed the reactive
// compaction threshold (80% of model limit). This is checked after every
// API response.
func (tb *TokenBudget) ShouldCompactReactive() bool {
	return tb.GetUsagePercent() >= 0.80
}

// ShouldCompactProactive returns true when the turn count has reached a
// multiple of the given interval AND usage is above 50%. This catches
// slow-growing sessions before they hit the reactive threshold.
func (tb *TokenBudget) ShouldCompactProactive(interval int) bool {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	if interval <= 0 {
		interval = 20
	}
	if tb.TurnCount == 0 || tb.TurnCount%interval != 0 {
		return false
	}
	// Only compact proactively if we are above 50% usage
	used := tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens
	return float64(used)/float64(tb.ModelLimit) >= 0.50
}

// IncrementCompactCount records that a compaction occurred.
func (tb *TokenBudget) IncrementCompactCount() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.CompactCount++
}

// GetWarningLevel returns a severity string based on current usage.
// Returns "ok", "warn" (75%), "caution" (85%), or "critical" (95%).
func (tb *TokenBudget) GetWarningLevel() string {
	pct := tb.GetUsagePercent()
	switch {
	case pct >= 0.95:
		return "critical"
	case pct >= 0.85:
		return "caution"
	case pct >= 0.75:
		return "warn"
	default:
		return "ok"
	}
}

// Summary returns a human-readable one-line summary of the budget state.
func (tb *TokenBudget) Summary() string {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	used := tb.SystemPromptTokens + tb.ToolDefinitionTokens + tb.HistoryTokens
	return fmt.Sprintf("%s/%s (%.1f%%)",
		FormatTokenCount(used),
		FormatTokenCount(tb.ModelLimit),
		float64(used)/float64(tb.ModelLimit)*100,
	)
}

// --- Helpers migrated from config/tokens.go ---

// GetModelLimit returns the token limit for a model name.
// Falls back to the "default" entry if the model is not found.
func GetModelLimit(model string) int {
	if limit, ok := ModelLimits[model]; ok {
		return limit
	}
	return ModelLimits["default"]
}

// GetModelLimitWithOverride returns the token limit for a model, using the
// config override if it is positive.
func GetModelLimitWithOverride(model string, configOverride int) int {
	if configOverride > 0 {
		return configOverride
	}
	return GetModelLimit(model)
}

// EstimateTokens approximates token count from text length.
// Uses the rough heuristic of 4 characters per token.
func EstimateTokens(text string) int {
	return len(text) / 4
}

// FormatTokenCount formats a token count with K/M suffix for display.
func FormatTokenCount(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	} else if tokens >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}
```

### Tests

```go
// cmd/celeste/context/budget_test.go
package context

import (
	"testing"
)

func TestNewTokenBudget(t *testing.T) {
	tb := NewTokenBudget(128000, 2000, 500)
	if tb.ModelLimit != 128000 {
		t.Errorf("ModelLimit = %d, want 128000", tb.ModelLimit)
	}
	if tb.SystemPromptTokens != 2000 {
		t.Errorf("SystemPromptTokens = %d, want 2000", tb.SystemPromptTokens)
	}
	if tb.ToolDefinitionTokens != 500 {
		t.Errorf("ToolDefinitionTokens = %d, want 500", tb.ToolDefinitionTokens)
	}
	if tb.TurnCount != 0 {
		t.Errorf("TurnCount = %d, want 0", tb.TurnCount)
	}
}

func TestNewTokenBudget_ZeroLimit(t *testing.T) {
	tb := NewTokenBudget(0, 100, 50)
	if tb.ModelLimit != ModelLimits["default"] {
		t.Errorf("ModelLimit = %d, want default %d", tb.ModelLimit, ModelLimits["default"])
	}
}

func TestNewTokenBudgetForModel(t *testing.T) {
	tb := NewTokenBudgetForModel("gpt-4o", 1000, 500)
	if tb.ModelLimit != 128000 {
		t.Errorf("ModelLimit = %d, want 128000", tb.ModelLimit)
	}

	tb2 := NewTokenBudgetForModel("unknown-model", 100, 50)
	if tb2.ModelLimit != ModelLimits["default"] {
		t.Errorf("ModelLimit for unknown = %d, want default %d", tb2.ModelLimit, ModelLimits["default"])
	}
}

func TestAddTurn(t *testing.T) {
	tb := NewTokenBudget(128000, 2000, 500)
	tb.AddTurn(5000, 1000)

	if tb.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", tb.TurnCount)
	}
	if tb.LastPromptTokens != 5000 {
		t.Errorf("LastPromptTokens = %d, want 5000", tb.LastPromptTokens)
	}
	if tb.LastCompTokens != 1000 {
		t.Errorf("LastCompTokens = %d, want 1000", tb.LastCompTokens)
	}
	// History = prompt - overhead = 5000 - 2500 = 2500
	if tb.HistoryTokens != 2500 {
		t.Errorf("HistoryTokens = %d, want 2500", tb.HistoryTokens)
	}
}

func TestAddTurn_PromptLessThanOverhead(t *testing.T) {
	tb := NewTokenBudget(128000, 5000, 3000)
	// Prompt tokens less than overhead -- should not go negative
	tb.AddTurn(1000, 500)
	if tb.HistoryTokens != 0 {
		t.Errorf("HistoryTokens = %d, want 0 (should not go negative)", tb.HistoryTokens)
	}
}

func TestTotalUsed(t *testing.T) {
	tb := NewTokenBudget(128000, 2000, 500)
	tb.AddTurn(12500, 2000) // history = 12500 - 2500 = 10000
	// Total = 2000 + 500 + 10000 = 12500
	if tb.TotalUsed() != 12500 {
		t.Errorf("TotalUsed = %d, want 12500", tb.TotalUsed())
	}
}

func TestAvailable(t *testing.T) {
	tb := NewTokenBudget(10000, 1000, 500)
	tb.SetHistoryTokens(3000)
	// Available = 10000 - (1000 + 500 + 3000) = 5500
	if tb.Available() != 5500 {
		t.Errorf("Available = %d, want 5500", tb.Available())
	}
}

func TestAvailable_Overbudget(t *testing.T) {
	tb := NewTokenBudget(1000, 500, 300)
	tb.SetHistoryTokens(500)
	// Used = 1300, limit = 1000, available should be 0 (not negative)
	if tb.Available() != 0 {
		t.Errorf("Available = %d, want 0 when overbudget", tb.Available())
	}
}

func TestGetUsagePercent(t *testing.T) {
	tb := NewTokenBudget(10000, 1000, 500)
	tb.SetHistoryTokens(6500)
	// Used = 8000, pct = 0.80
	pct := tb.GetUsagePercent()
	if pct < 0.799 || pct > 0.801 {
		t.Errorf("GetUsagePercent = %f, want ~0.80", pct)
	}
}

func TestGetUsagePercent_ZeroLimit(t *testing.T) {
	tb := &TokenBudget{ModelLimit: 0}
	if tb.GetUsagePercent() != 0.0 {
		t.Errorf("GetUsagePercent with zero limit should be 0.0")
	}
}

func TestShouldCompactReactive(t *testing.T) {
	tb := NewTokenBudget(10000, 1000, 500)

	// 70% -- should NOT trigger
	tb.SetHistoryTokens(5500) // total = 7000
	if tb.ShouldCompactReactive() {
		t.Error("ShouldCompactReactive should be false at 70%")
	}

	// 80% -- SHOULD trigger
	tb.SetHistoryTokens(6500) // total = 8000
	if !tb.ShouldCompactReactive() {
		t.Error("ShouldCompactReactive should be true at 80%")
	}

	// 95% -- SHOULD trigger
	tb.SetHistoryTokens(8000) // total = 9500
	if !tb.ShouldCompactReactive() {
		t.Error("ShouldCompactReactive should be true at 95%")
	}
}

func TestShouldCompactProactive(t *testing.T) {
	tb := NewTokenBudget(10000, 1000, 500)

	// At turn 0 -- never
	if tb.ShouldCompactProactive(20) {
		t.Error("ShouldCompactProactive should be false at turn 0")
	}

	// Simulate 20 turns at 55% usage
	tb.SetHistoryTokens(4000) // total = 5500 = 55%
	tb.mu.Lock()
	tb.TurnCount = 20
	tb.mu.Unlock()

	if !tb.ShouldCompactProactive(20) {
		t.Error("ShouldCompactProactive should be true at turn 20 with 55% usage")
	}

	// At turn 19 -- should NOT trigger
	tb.mu.Lock()
	tb.TurnCount = 19
	tb.mu.Unlock()
	if tb.ShouldCompactProactive(20) {
		t.Error("ShouldCompactProactive should be false at turn 19")
	}

	// At turn 20 but only 30% usage -- should NOT trigger
	tb.mu.Lock()
	tb.TurnCount = 20
	tb.mu.Unlock()
	tb.SetHistoryTokens(1500) // total = 3000 = 30%
	if tb.ShouldCompactProactive(20) {
		t.Error("ShouldCompactProactive should be false at 30% even on interval turn")
	}
}

func TestGetWarningLevel(t *testing.T) {
	tb := NewTokenBudget(10000, 0, 0)

	tests := []struct {
		history int
		want    string
	}{
		{5000, "ok"},
		{7500, "warn"},
		{8500, "caution"},
		{9500, "critical"},
	}
	for _, tt := range tests {
		tb.SetHistoryTokens(tt.history)
		got := tb.GetWarningLevel()
		if got != tt.want {
			t.Errorf("GetWarningLevel at %d tokens = %q, want %q", tt.history, got, tt.want)
		}
	}
}

func TestGetModelLimit(t *testing.T) {
	if GetModelLimit("gpt-4o") != 128000 {
		t.Errorf("gpt-4o limit wrong")
	}
	if GetModelLimit("nonexistent") != ModelLimits["default"] {
		t.Errorf("fallback limit wrong")
	}
}

func TestEstimateTokens(t *testing.T) {
	// 20 characters -> 5 tokens
	if EstimateTokens("12345678901234567890") != 5 {
		t.Errorf("EstimateTokens(20 chars) = %d, want 5", EstimateTokens("12345678901234567890"))
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{500, "500"},
		{1500, "1.5K"},
		{128000, "128.0K"},
		{1500000, "1.5M"},
	}
	for _, tt := range tests {
		got := FormatTokenCount(tt.in)
		if got != tt.want {
			t.Errorf("FormatTokenCount(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSummary(t *testing.T) {
	tb := NewTokenBudget(128000, 2000, 500)
	tb.SetHistoryTokens(10000)
	s := tb.Summary()
	// Should contain formatted numbers and percentage
	if s == "" {
		t.Error("Summary should not be empty")
	}
}
```

---

## Task 2: Tool result capping

**File:** `cmd/celeste/context/limits.go`

- [ ] Define `DefaultMaxToolResultBytes` constant (32768 = 32KB)
- [ ] Define `ToolResultsDir` path helper (`~/.celeste/tool-results`)
- [ ] Implement `CapToolResult(result, maxBytes, sessionID, toolCallID) (string, bool, error)`
- [ ] When a result exceeds maxBytes: write the full result to disk, return a truncated preview with the file path
- [ ] Write tests using `t.TempDir()` to avoid filesystem pollution

### Complete implementation

```go
// cmd/celeste/context/limits.go
package context

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DefaultMaxToolResultBytes is the maximum size in bytes for a single tool
	// result before it gets capped and spilled to disk. 32KB.
	DefaultMaxToolResultBytes = 32 * 1024

	// previewTailBytes controls how many bytes from the end of the result are
	// included in the preview (so the model sees both the beginning and end).
	previewTailBytes = 512
)

// ToolResultsBaseDir returns the base directory for spilled tool results.
// Default: ~/.celeste/tool-results
func ToolResultsBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".celeste", "tool-results"), nil
}

// CapToolResult checks whether a tool result exceeds maxBytes. If it does, the
// full result is written to disk at:
//
//	{baseDir}/{sessionID}/{toolCallID}.txt
//
// and a truncated preview is returned containing the first portion, a notice
// with the file path, and the last previewTailBytes of the result.
//
// If baseDir is empty, ToolResultsBaseDir() is used.
//
// Returns:
//   - capped: the (possibly truncated) result string to send to the model
//   - wasCapped: true if the result was truncated
//   - err: any I/O error from writing the spill file
func CapToolResult(result string, maxBytes int, sessionID, toolCallID, baseDir string) (capped string, wasCapped bool, err error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxToolResultBytes
	}

	if len(result) <= maxBytes {
		return result, false, nil
	}

	// Determine spill directory
	if baseDir == "" {
		baseDir, err = ToolResultsBaseDir()
		if err != nil {
			return result, false, err
		}
	}

	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return result, false, fmt.Errorf("create tool-results dir: %w", err)
	}

	spillPath := filepath.Join(sessionDir, toolCallID+".txt")
	if err := os.WriteFile(spillPath, []byte(result), 0644); err != nil {
		return result, false, fmt.Errorf("write spill file: %w", err)
	}

	// Build the capped preview:
	//   [first N bytes]
	//   --- TRUNCATED (full output: {spillPath}, {total} bytes) ---
	//   [last previewTailBytes bytes]
	totalBytes := len(result)

	// Reserve space for the notice and tail in the budget
	notice := fmt.Sprintf(
		"\n\n--- TRUNCATED (%d bytes total, full output saved to: %s) ---\n\n",
		totalBytes, spillPath,
	)
	noticeLen := len(notice)
	tailLen := previewTailBytes
	if tailLen > totalBytes {
		tailLen = totalBytes
	}

	headLen := maxBytes - noticeLen - tailLen
	if headLen < 256 {
		headLen = 256 // Ensure a minimum head size
	}
	if headLen > totalBytes {
		headLen = totalBytes
	}

	tail := result[totalBytes-tailLen:]
	head := result[:headLen]

	capped = head + notice + tail
	return capped, true, nil
}
```

### Tests

```go
// cmd/celeste/context/limits_test.go
package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapToolResult_UnderLimit(t *testing.T) {
	result := "short result"
	capped, wasCapped, err := CapToolResult(result, 1024, "sess1", "tc1", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasCapped {
		t.Error("wasCapped should be false for short result")
	}
	if capped != result {
		t.Errorf("capped = %q, want %q", capped, result)
	}
}

func TestCapToolResult_OverLimit(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a 64KB result
	result := strings.Repeat("x", 64*1024)

	capped, wasCapped, err := CapToolResult(result, 32*1024, "sess1", "tc42", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !wasCapped {
		t.Error("wasCapped should be true for oversized result")
	}

	// Verify capped result contains truncation notice
	if !strings.Contains(capped, "TRUNCATED") {
		t.Error("capped result should contain TRUNCATED notice")
	}
	if !strings.Contains(capped, "tc42.txt") {
		t.Error("capped result should contain spill file path")
	}
	if !strings.Contains(capped, "65536 bytes total") {
		t.Error("capped result should contain total byte count")
	}

	// Verify the spill file was written with full content
	spillPath := filepath.Join(tmpDir, "sess1", "tc42.txt")
	data, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("failed to read spill file: %v", err)
	}
	if len(data) != 64*1024 {
		t.Errorf("spill file size = %d, want %d", len(data), 64*1024)
	}
}

func TestCapToolResult_ExactlyAtLimit(t *testing.T) {
	result := strings.Repeat("a", 1024)
	capped, wasCapped, err := CapToolResult(result, 1024, "s", "t", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasCapped {
		t.Error("should not cap result exactly at limit")
	}
	if capped != result {
		t.Error("result should be unchanged when exactly at limit")
	}
}

func TestCapToolResult_CreatesSessionDir(t *testing.T) {
	tmpDir := t.TempDir()
	result := strings.Repeat("z", 2048)

	_, _, err := CapToolResult(result, 512, "new-session", "tc1", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessionDir := filepath.Join(tmpDir, "new-session")
	info, err := os.Stat(sessionDir)
	if err != nil {
		t.Fatalf("session dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("session dir should be a directory")
	}
}

func TestCapToolResult_DefaultMaxBytes(t *testing.T) {
	// Pass 0 for maxBytes -- should use DefaultMaxToolResultBytes
	result := strings.Repeat("y", DefaultMaxToolResultBytes+100)
	_, wasCapped, err := CapToolResult(result, 0, "s", "t", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !wasCapped {
		t.Error("should cap when using default and result exceeds it")
	}
}
```

---

## Task 3: Compaction engine

**File:** `cmd/celeste/context/compaction.go`

- [ ] Define `CompactionEngine` struct with `Summarizer` interface dependency
- [ ] Implement `CompactReactive(messages, budget) ([]ChatMessage, CompactionResult, error)`
- [ ] Implement `CompactProactive(messages, budget) ([]ChatMessage, CompactionResult, error)` (same logic, different trigger)
- [ ] Implement `CompactSnip(result, maxBytes) string` for inline truncation without disk spill
- [ ] Define `CompactionResult` struct for reporting what was done
- [ ] Write tests using a mock summarizer

### Complete implementation

```go
// cmd/celeste/context/compaction.go
package context

import (
	"context"
	"fmt"
	"time"
)

// ChatMessage mirrors tui.ChatMessage to avoid a circular import.
// The TUI layer converts between the two at integration boundaries.
type ChatMessage struct {
	Role       string
	Content    string
	ToolCallID string
	Name       string
	Timestamp  time.Time
}

// CompactionResult describes what a compaction operation did.
type CompactionResult struct {
	MessagesBefore int
	MessagesAfter  int
	TokensBefore   int // Estimated tokens in compacted messages
	TokensAfter    int // Estimated tokens in the summary replacement
	TurnsCompacted int // Number of user-assistant turn pairs summarized
}

// CompactionEngine orchestrates conversation compaction using a Summarizer.
type CompactionEngine struct {
	summarizer Summarizer
	// RecentTurnsToKeep controls how many recent user-assistant turn pairs
	// are protected from compaction. Default: 4.
	RecentTurnsToKeep int
}

// NewCompactionEngine creates a CompactionEngine with the given summarizer.
func NewCompactionEngine(summarizer Summarizer) *CompactionEngine {
	return &CompactionEngine{
		summarizer:        summarizer,
		RecentTurnsToKeep: 4,
	}
}

// CompactReactive performs reactive compaction when the context budget crosses
// the 80% threshold. It summarizes the oldest messages while keeping the most
// recent RecentTurnsToKeep turn pairs intact.
//
// The messages slice should be the full conversation history (excluding system
// prompt, which is sent separately). The budget is used to track compaction
// count and recalculate usage after compaction.
func (ce *CompactionEngine) CompactReactive(ctx context.Context, messages []ChatMessage, budget *TokenBudget) ([]ChatMessage, CompactionResult, error) {
	return ce.compact(ctx, messages, budget)
}

// CompactProactive performs proactive compaction. The logic is identical to
// reactive compaction but is triggered on a turn-count interval rather than
// a usage threshold. The caller is responsible for checking
// budget.ShouldCompactProactive() before calling this.
func (ce *CompactionEngine) CompactProactive(ctx context.Context, messages []ChatMessage, budget *TokenBudget) ([]ChatMessage, CompactionResult, error) {
	return ce.compact(ctx, messages, budget)
}

// compact is the shared compaction implementation.
func (ce *CompactionEngine) compact(ctx context.Context, messages []ChatMessage, budget *TokenBudget) ([]ChatMessage, CompactionResult, error) {
	result := CompactionResult{
		MessagesBefore: len(messages),
	}

	if len(messages) == 0 {
		return messages, result, fmt.Errorf("no messages to compact")
	}

	// Find the split point: protect the last N turn pairs.
	// A "turn pair" is a user message followed by an assistant message.
	// We also protect any system messages at the start.
	keepCount := ce.RecentTurnsToKeep
	if keepCount <= 0 {
		keepCount = 4
	}

	splitIdx := findSplitIndex(messages, keepCount)
	if splitIdx <= 0 {
		return messages, result, fmt.Errorf("not enough messages to compact (need more than %d turn pairs)", keepCount)
	}

	// The messages to summarize are messages[0:splitIdx].
	// The messages to keep are messages[splitIdx:].
	toSummarize := messages[:splitIdx]
	toKeep := messages[splitIdx:]

	// Estimate tokens before compaction
	for _, msg := range toSummarize {
		result.TokensBefore += EstimateTokens(msg.Content) + 4 // +4 for role overhead
	}

	// Count turn pairs being compacted
	for _, msg := range toSummarize {
		if msg.Role == "user" {
			result.TurnsCompacted++
		}
	}

	// Summarize
	summary, err := ce.summarizer.Summarize(ctx, toSummarize)
	if err != nil {
		return messages, result, fmt.Errorf("summarization failed: %w", err)
	}

	// Build the compacted message list
	summaryMsg := ChatMessage{
		Role:      "system",
		Content:   fmt.Sprintf("[Conversation Summary - %d turns compacted]\n\n%s", result.TurnsCompacted, summary),
		Timestamp: time.Now(),
	}

	result.TokensAfter = EstimateTokens(summaryMsg.Content) + 4

	compacted := make([]ChatMessage, 0, 1+len(toKeep))
	compacted = append(compacted, summaryMsg)
	compacted = append(compacted, toKeep...)

	result.MessagesAfter = len(compacted)

	// Update budget
	if budget != nil {
		budget.IncrementCompactCount()
		// Recalculate history tokens: subtract what we removed, add the summary
		saved := result.TokensBefore - result.TokensAfter
		budget.mu.Lock()
		budget.HistoryTokens -= saved
		if budget.HistoryTokens < 0 {
			budget.HistoryTokens = 0
		}
		budget.mu.Unlock()
	}

	return compacted, result, nil
}

// findSplitIndex returns the index that separates "old" messages from the
// most recent keepTurnPairs turn pairs. Messages before the split index
// will be summarized; messages from the split index onward are kept.
func findSplitIndex(messages []ChatMessage, keepTurnPairs int) int {
	// Walk backwards counting user messages (each user message starts a turn pair)
	turnsFound := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			turnsFound++
			if turnsFound >= keepTurnPairs {
				return i
			}
		}
	}
	// Not enough turn pairs to split
	return 0
}

// CompactSnip performs inline truncation of a single string without writing
// to disk. This is a lightweight alternative to CapToolResult when you do not
// need persistent storage of the full result (e.g., for intermediate
// processing steps).
func CompactSnip(text string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxToolResultBytes
	}
	if len(text) <= maxBytes {
		return text
	}

	tailSize := 256
	if tailSize > len(text) {
		tailSize = len(text)
	}

	headSize := maxBytes - tailSize - 60 // 60 bytes for the notice
	if headSize < 128 {
		headSize = 128
	}
	if headSize > len(text) {
		headSize = len(text)
	}

	head := text[:headSize]
	tail := text[len(text)-tailSize:]
	notice := fmt.Sprintf("\n[...snipped %d bytes...]\n", len(text)-headSize-tailSize)

	return head + notice + tail
}

// FormatCompactionResult creates a user-friendly summary of what compaction did.
func FormatCompactionResult(r CompactionResult) string {
	tokensSaved := r.TokensBefore - r.TokensAfter
	if r.TokensBefore == 0 {
		return fmt.Sprintf("Compacted: %d msgs -> %d msgs", r.MessagesBefore, r.MessagesAfter)
	}
	savingsPct := float64(tokensSaved) / float64(r.TokensBefore) * 100
	return fmt.Sprintf(
		"Compacted: %d msgs -> %d msgs (%d turns summarized, saved ~%s tokens, %.0f%% reduction)",
		r.MessagesBefore, r.MessagesAfter,
		r.TurnsCompacted,
		FormatTokenCount(tokensSaved),
		savingsPct,
	)
}
```

### Tests

```go
// cmd/celeste/context/compaction_test.go
package context

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockSummarizer implements Summarizer for testing.
type mockSummarizer struct {
	summary string
	err     error
	called  int
}

func (m *mockSummarizer) Summarize(ctx context.Context, messages []ChatMessage) (string, error) {
	m.called++
	return m.summary, m.err
}

func makeConversation(turnPairs int) []ChatMessage {
	var msgs []ChatMessage
	for i := 0; i < turnPairs; i++ {
		msgs = append(msgs, ChatMessage{
			Role:      "user",
			Content:   fmt.Sprintf("User message %d with some content to estimate tokens from", i+1),
			Timestamp: time.Now().Add(time.Duration(i*2) * time.Minute),
		})
		msgs = append(msgs, ChatMessage{
			Role:      "assistant",
			Content:   fmt.Sprintf("Assistant response %d with detailed answer content here", i+1),
			Timestamp: time.Now().Add(time.Duration(i*2+1) * time.Minute),
		})
	}
	return msgs
}

func TestCompactReactive_Basic(t *testing.T) {
	mock := &mockSummarizer{summary: "This is a summary of the conversation."}
	engine := NewCompactionEngine(mock)
	engine.RecentTurnsToKeep = 2

	msgs := makeConversation(6) // 12 messages, 6 turn pairs
	budget := NewTokenBudget(10000, 500, 200)

	ctx := context.Background()
	compacted, result, err := engine.CompactReactive(ctx, msgs, budget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.called != 1 {
		t.Errorf("summarizer called %d times, want 1", mock.called)
	}

	// Should have: 1 summary + last 2 turn pairs (4 messages) = 5
	if len(compacted) != 5 {
		t.Errorf("compacted length = %d, want 5", len(compacted))
	}

	// First message should be the summary
	if compacted[0].Role != "system" {
		t.Errorf("first message role = %q, want system", compacted[0].Role)
	}
	if !strings.Contains(compacted[0].Content, "Conversation Summary") {
		t.Error("summary message should contain 'Conversation Summary'")
	}

	// Result should have correct counts
	if result.MessagesBefore != 12 {
		t.Errorf("MessagesBefore = %d, want 12", result.MessagesBefore)
	}
	if result.MessagesAfter != 5 {
		t.Errorf("MessagesAfter = %d, want 5", result.MessagesAfter)
	}
	if result.TurnsCompacted != 4 {
		t.Errorf("TurnsCompacted = %d, want 4", result.TurnsCompacted)
	}

	// Budget should have been updated
	if budget.CompactCount != 1 {
		t.Errorf("CompactCount = %d, want 1", budget.CompactCount)
	}
}

func TestCompactReactive_TooFewMessages(t *testing.T) {
	mock := &mockSummarizer{summary: "summary"}
	engine := NewCompactionEngine(mock)
	engine.RecentTurnsToKeep = 4

	// Only 3 turn pairs, need 4 to keep -- nothing to compact
	msgs := makeConversation(3)
	budget := NewTokenBudget(10000, 500, 200)

	ctx := context.Background()
	_, _, err := engine.CompactReactive(ctx, msgs, budget)
	if err == nil {
		t.Error("expected error when too few messages to compact")
	}
}

func TestCompactReactive_SummarizerError(t *testing.T) {
	mock := &mockSummarizer{err: fmt.Errorf("LLM unavailable")}
	engine := NewCompactionEngine(mock)
	engine.RecentTurnsToKeep = 2

	msgs := makeConversation(6)
	budget := NewTokenBudget(10000, 500, 200)

	ctx := context.Background()
	returned, _, err := engine.CompactReactive(ctx, msgs, budget)
	if err == nil {
		t.Error("expected error when summarizer fails")
	}
	// Should return original messages unchanged
	if len(returned) != len(msgs) {
		t.Errorf("should return original messages on error, got %d want %d", len(returned), len(msgs))
	}
}

func TestCompactReactive_EmptyMessages(t *testing.T) {
	mock := &mockSummarizer{summary: "summary"}
	engine := NewCompactionEngine(mock)

	ctx := context.Background()
	_, _, err := engine.CompactReactive(ctx, nil, nil)
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestCompactReactive_NilBudget(t *testing.T) {
	mock := &mockSummarizer{summary: "A summary."}
	engine := NewCompactionEngine(mock)
	engine.RecentTurnsToKeep = 1

	msgs := makeConversation(4)
	ctx := context.Background()

	// Should not panic with nil budget
	compacted, _, err := engine.CompactReactive(ctx, msgs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compacted) == 0 {
		t.Error("compacted should not be empty")
	}
}

func TestCompactSnip_UnderLimit(t *testing.T) {
	text := "short text"
	result := CompactSnip(text, 1024)
	if result != text {
		t.Errorf("CompactSnip should not modify text under limit")
	}
}

func TestCompactSnip_OverLimit(t *testing.T) {
	text := strings.Repeat("a", 10000)
	result := CompactSnip(text, 1024)

	if len(result) >= len(text) {
		t.Error("CompactSnip result should be shorter than input")
	}
	if !strings.Contains(result, "snipped") {
		t.Error("CompactSnip result should contain snip notice")
	}
	// Should contain the tail of the original text
	if !strings.HasSuffix(result, strings.Repeat("a", 256)) {
		t.Error("CompactSnip result should end with tail of original")
	}
}

func TestFormatCompactionResult(t *testing.T) {
	r := CompactionResult{
		MessagesBefore: 20,
		MessagesAfter:  6,
		TokensBefore:   5000,
		TokensAfter:    300,
		TurnsCompacted: 7,
	}
	formatted := FormatCompactionResult(r)
	if !strings.Contains(formatted, "20 msgs") {
		t.Error("should contain message count before")
	}
	if !strings.Contains(formatted, "6 msgs") {
		t.Error("should contain message count after")
	}
	if !strings.Contains(formatted, "7 turns") {
		t.Error("should contain turns compacted")
	}
}

func TestFindSplitIndex(t *testing.T) {
	msgs := makeConversation(5) // 10 messages

	// Keep last 2 turn pairs -- split should be at index 6 (user message of 4th pair)
	idx := findSplitIndex(msgs, 2)
	if idx != 6 {
		t.Errorf("findSplitIndex(keep=2) = %d, want 6", idx)
	}

	// Keep last 5 -- all messages are recent, split at 0
	idx = findSplitIndex(msgs, 5)
	if idx != 0 {
		t.Errorf("findSplitIndex(keep=5) = %d, want 0", idx)
	}

	// Keep last 1
	idx = findSplitIndex(msgs, 1)
	if idx != 8 {
		t.Errorf("findSplitIndex(keep=1) = %d, want 8", idx)
	}
}
```

---

## Task 4: Summarizer migration

**File:** `cmd/celeste/context/summarizer.go`

- [ ] Define `Summarizer` interface with `Summarize(ctx, messages) (string, error)`
- [ ] Implement `LLMSummarizer` struct that calls the LLM with a summarization prompt
- [ ] Define `LLMClient` interface for dependency injection (avoids importing `llm` package directly)
- [ ] Migrate prompt and logic from `llm/summarize.go`
- [ ] Write tests with mock LLM client

### Complete implementation

```go
// cmd/celeste/context/summarizer.go
package context

import (
	"context"
	"fmt"
	"strings"
)

// Summarizer creates concise summaries of conversation message sequences.
// This is the core interface used by CompactionEngine.
type Summarizer interface {
	// Summarize takes a sequence of messages and returns a text summary
	// that preserves key context, decisions, and technical details.
	Summarize(ctx context.Context, messages []ChatMessage) (string, error)
}

// LLMClient is the minimal interface required to call an LLM for summarization.
// This avoids importing the llm package directly, preventing circular deps.
// The concrete implementation in main.go or wherever the LLM client is created
// should satisfy this interface.
type LLMClient interface {
	// SendSummarizationRequest sends a system+user prompt pair to the LLM
	// and returns the text response. No tool calling, no streaming.
	SendSummarizationRequest(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// LLMSummarizer implements Summarizer by calling an LLM to generate summaries.
// Migrated from llm/summarize.go.
type LLMSummarizer struct {
	client LLMClient
}

// NewLLMSummarizer creates a new LLM-backed summarizer.
func NewLLMSummarizer(client LLMClient) *LLMSummarizer {
	return &LLMSummarizer{client: client}
}

// summarizationSystemPrompt is the system prompt sent to the LLM for
// conversation summarization. It instructs the model to produce a concise
// summary preserving key context.
const summarizationSystemPrompt = `You are a conversation summarizer. Create a concise summary of the following conversation that preserves:
1. Key topics discussed
2. Important decisions or conclusions reached
3. Any action items or next steps
4. Essential context needed to continue the conversation
5. Technical details or specific information mentioned (file paths, function names, error messages, etc.)

The summary should be 150-250 words and written in a clear, factual style.
Do NOT include phrases like "the user asked" or "the assistant responded" -- just state the facts and conclusions directly.`

// Summarize creates a summary of the given messages by sending them to the LLM.
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []ChatMessage) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to summarize")
	}

	// Build conversation text
	var b strings.Builder
	for _, msg := range messages {
		fmt.Fprintf(&b, "[%s]: %s\n\n", msg.Role, msg.Content)
	}

	userPrompt := fmt.Sprintf("Summarize the following conversation (%d messages):\n\n%s",
		len(messages), b.String())

	summary, err := s.client.SendSummarizationRequest(ctx, summarizationSystemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("summarization request failed: %w", err)
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", fmt.Errorf("LLM returned empty summary")
	}

	return summary, nil
}

// EstimateSummarySavings estimates tokens before and after summarization for
// the given message count. Useful for previewing compaction impact without
// actually calling the LLM.
func EstimateSummarySavings(messages []ChatMessage, count int) (before, after int) {
	if len(messages) == 0 || count <= 0 {
		return 0, 0
	}
	if count > len(messages) {
		count = len(messages)
	}

	for _, msg := range messages[:count] {
		before += EstimateTokens(msg.Content) + 4
	}

	// A good summary is typically 150-250 words, roughly 200-350 tokens.
	// We use 300 as the estimate plus formatting overhead.
	after = 300 + 50
	return before, after
}
```

### Tests

```go
// cmd/celeste/context/summarizer_test.go
package context

import (
	"context"
	"fmt"
	"testing"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	response string
	err      error
	lastSys  string
	lastUser string
}

func (m *mockLLMClient) SendSummarizationRequest(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	m.lastSys = systemPrompt
	m.lastUser = userPrompt
	return m.response, m.err
}

func TestLLMSummarizer_Summarize(t *testing.T) {
	client := &mockLLMClient{response: "The conversation covered Go testing patterns and error handling."}
	summarizer := NewLLMSummarizer(client)

	msgs := []ChatMessage{
		{Role: "user", Content: "How do I write tests in Go?"},
		{Role: "assistant", Content: "Use the testing package with Test functions."},
		{Role: "user", Content: "What about error handling?"},
		{Role: "assistant", Content: "Use the errors package and wrap errors with fmt.Errorf."},
	}

	ctx := context.Background()
	summary, err := summarizer.Summarize(ctx, msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "The conversation covered Go testing patterns and error handling." {
		t.Errorf("unexpected summary: %s", summary)
	}
	// Verify the system prompt was sent
	if client.lastSys == "" {
		t.Error("system prompt should not be empty")
	}
	// Verify user prompt contains message count
	if client.lastUser == "" {
		t.Error("user prompt should not be empty")
	}
}

func TestLLMSummarizer_EmptyMessages(t *testing.T) {
	client := &mockLLMClient{response: "summary"}
	summarizer := NewLLMSummarizer(client)

	ctx := context.Background()
	_, err := summarizer.Summarize(ctx, nil)
	if err == nil {
		t.Error("expected error for nil messages")
	}

	_, err = summarizer.Summarize(ctx, []ChatMessage{})
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestLLMSummarizer_LLMError(t *testing.T) {
	client := &mockLLMClient{err: fmt.Errorf("API rate limit exceeded")}
	summarizer := NewLLMSummarizer(client)

	msgs := []ChatMessage{{Role: "user", Content: "hello"}}
	ctx := context.Background()
	_, err := summarizer.Summarize(ctx, msgs)
	if err == nil {
		t.Error("expected error when LLM fails")
	}
}

func TestLLMSummarizer_EmptyResponse(t *testing.T) {
	client := &mockLLMClient{response: "   "}
	summarizer := NewLLMSummarizer(client)

	msgs := []ChatMessage{{Role: "user", Content: "hello"}}
	ctx := context.Background()
	_, err := summarizer.Summarize(ctx, msgs)
	if err == nil {
		t.Error("expected error for whitespace-only response")
	}
}

func TestEstimateSummarySavings(t *testing.T) {
	msgs := makeConversation(10) // Uses the helper from compaction_test.go

	before, after := EstimateSummarySavings(msgs, 10)
	if before <= 0 {
		t.Error("before should be positive")
	}
	if after <= 0 {
		t.Error("after should be positive")
	}
	if after >= before {
		t.Errorf("summary should use fewer tokens than originals: before=%d after=%d", before, after)
	}
}

func TestEstimateSummarySavings_EdgeCases(t *testing.T) {
	before, after := EstimateSummarySavings(nil, 5)
	if before != 0 || after != 0 {
		t.Error("nil messages should return 0, 0")
	}

	before, after = EstimateSummarySavings([]ChatMessage{}, 0)
	if before != 0 || after != 0 {
		t.Error("zero count should return 0, 0")
	}

	// count > len(messages) -- should clamp
	msgs := makeConversation(2) // 4 messages
	before, after = EstimateSummarySavings(msgs, 100)
	if before <= 0 {
		t.Error("should still estimate with clamped count")
	}
}
```

---

## Task 5: Wire into TUI

**Files modified:** `cmd/celeste/tui/app.go`, `cmd/celeste/main.go`

- [ ] In `cmd/celeste/tui/app.go`:
  - [ ] Replace `contextTracker *config.ContextTracker` with `tokenBudget *ctxpkg.TokenBudget` (import alias `ctxpkg` to avoid collision with stdlib `context`)
  - [ ] In the `AppModel` constructor (or init function), create the budget: `NewTokenBudgetForModel(model, systemPromptTokens, toolDefTokens)`
  - [ ] In the `StreamDoneMsg` handler, call `budget.AddTurn(usage.PromptTokens, usage.CompletionTokens)`
  - [ ] After `AddTurn`, check `budget.ShouldCompactReactive()` -- if true, trigger compaction
  - [ ] Update status bar rendering to use `budget.Summary()` and `budget.GetWarningLevel()`
- [ ] In `cmd/celeste/main.go`:
  - [ ] In `TUIClientAdapter` (or equivalent tool result handler), call `CapToolResult()` before returning tool results to the model
  - [ ] Wire up `LLMSummarizer` using an adapter that wraps the existing `llm.Client`

### Key code snippets

**AppModel field change** (`tui/app.go`):

```go
import ctxpkg "github.com/whykusanagi/celeste-cli/cmd/celeste/context"

type AppModel struct {
    // ... existing fields ...

    // Context tracking -- replaces contextTracker *config.ContextTracker
    tokenBudget      *ctxpkg.TokenBudget
    compactionEngine *ctxpkg.CompactionEngine
}
```

**StreamDoneMsg handler** (`tui/app.go`, inside the `Update` method):

```go
case StreamDoneMsg:
    m.streaming = false

    // Update token budget from API usage
    if msg.Usage != nil && m.tokenBudget != nil {
        m.tokenBudget.AddTurn(msg.Usage.PromptTokens, msg.Usage.CompletionTokens)

        // Check reactive compaction
        if m.tokenBudget.ShouldCompactReactive() && m.compactionEngine != nil {
            // Convert session messages to context.ChatMessage
            // Run compaction, update session messages
            // Log result to status bar
        }
    }

    // Update status bar with budget info
    if m.tokenBudget != nil {
        m.status.SetInfo(m.tokenBudget.Summary())
    }
```

**Tool result capping** (`main.go` or tool execution layer):

```go
// In the tool result handler, before returning result to the model:
capped, wasCapped, err := ctxpkg.CapToolResult(
    toolResult,
    ctxpkg.DefaultMaxToolResultBytes,
    session.ID,
    toolCallID,
    "", // use default base dir
)
if err != nil {
    log.Printf("warning: failed to cap tool result: %v", err)
    // Fall through with original result
} else if wasCapped {
    toolResult = capped
    log.Printf("Tool result capped: original %d bytes -> preview", len(toolResult))
}
```

**LLMClient adapter** (bridge between `context.LLMClient` and `llm.Client`):

```go
// llmSummarizerAdapter wraps llm.Client to satisfy context.LLMClient.
type llmSummarizerAdapter struct {
    client *llm.Client
}

func (a *llmSummarizerAdapter) SendSummarizationRequest(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
    msgs := []tui.ChatMessage{
        {Role: "system", Content: systemPrompt, Timestamp: time.Now()},
        {Role: "user", Content: userPrompt, Timestamp: time.Now()},
    }
    result, err := a.client.SendMessageSync(ctx, msgs, nil)
    if err != nil {
        return "", err
    }
    if result.Error != nil {
        return "", result.Error
    }
    return result.Content, nil
}
```

---

## Task 6: Wire into Agent

**File modified:** `cmd/celeste/agent/runtime.go`

- [ ] Add `tokenBudget *ctxpkg.TokenBudget` field to `Runner` struct
- [ ] Create budget in `NewRunner()` using model from config
- [ ] After each agent turn (tool call cycle), call `budget.AddTurn()` with token usage
- [ ] Between turns, check `budget.ShouldCompactReactive()` and `budget.ShouldCompactProactive(20)`
- [ ] If compaction triggers, run `CompactionEngine.CompactReactive()` and update the message history
- [ ] Cap tool results using `CapToolResult()` before appending to message history

### Key code snippets

**Runner struct update** (`agent/runtime.go`):

```go
import ctxpkg "github.com/whykusanagi/celeste-cli/cmd/celeste/context"

type Runner struct {
    client           *llm.Client
    registry         *skills.Registry
    store            *CheckpointStore
    options          Options
    out              io.Writer
    errOut           io.Writer
    tokenBudget      *ctxpkg.TokenBudget
    compactionEngine *ctxpkg.CompactionEngine
}
```

**Budget initialization in NewRunner** (`agent/runtime.go`):

```go
// After creating the LLM client, estimate system prompt tokens and create budget
systemPromptTokens := ctxpkg.EstimateTokens(systemPrompt)
toolDefTokens := ctxpkg.EstimateTokens(toolDefinitionsJSON)
runner.tokenBudget = ctxpkg.NewTokenBudgetForModel(cfg.Model, systemPromptTokens, toolDefTokens)

// Create compaction engine with LLM summarizer
adapter := &llmSummarizerAdapter{client: runner.client}
summarizer := ctxpkg.NewLLMSummarizer(adapter)
runner.compactionEngine = ctxpkg.NewCompactionEngine(summarizer)
```

**Per-turn compaction check** (in the agent loop):

```go
// After receiving API response with usage data:
if usage != nil && r.tokenBudget != nil {
    r.tokenBudget.AddTurn(usage.PromptTokens, usage.CompletionTokens)

    shouldCompact := r.tokenBudget.ShouldCompactReactive() ||
        r.tokenBudget.ShouldCompactProactive(20)

    if shouldCompact && r.compactionEngine != nil {
        // Convert messages to context.ChatMessage, compact, convert back
        compacted, result, err := r.compactionEngine.CompactReactive(ctx, contextMsgs, r.tokenBudget)
        if err != nil {
            r.emitProgress(ProgressWarning, fmt.Sprintf("compaction failed: %v", err), turn, maxTurns)
        } else {
            // Replace message history with compacted version
            messages = convertBack(compacted)
            r.emitProgress(ProgressInfo, ctxpkg.FormatCompactionResult(result), turn, maxTurns)
        }
    }
}
```

**Tool result capping** (in tool execution):

```go
// After executing a tool and getting the result:
capped, wasCapped, err := ctxpkg.CapToolResult(
    result, ctxpkg.DefaultMaxToolResultBytes,
    sessionID, toolCallID, "",
)
if err == nil && wasCapped {
    result = capped
}
```

---

## Task 7: Delete old files and update imports

- [ ] Delete `cmd/celeste/config/context.go`
- [ ] Delete `cmd/celeste/config/tokens.go`
- [ ] Delete `cmd/celeste/llm/summarize.go`
- [ ] Search all `.go` files for imports of the deleted symbols:
  - `config.ContextTracker` -> `ctxpkg.TokenBudget`
  - `config.GetModelLimit` -> `ctxpkg.GetModelLimit`
  - `config.ModelLimits` -> `ctxpkg.ModelLimits`
  - `config.EstimateTokens` -> `ctxpkg.EstimateTokens`
  - `config.EstimateSessionTokens` -> update callers (session.go still uses this internally; either keep a thin wrapper in config or move it)
  - `config.FormatTokenCount` -> `ctxpkg.FormatTokenCount`
  - `config.TruncateToLimit` -> remove (replaced by compaction engine)
  - `llm.Summarizer` -> `ctxpkg.LLMSummarizer`
  - `llm.ShouldTriggerCompaction` -> `ctxpkg.TokenBudget.ShouldCompactReactive`
- [ ] Update `config/session.go` to use `ctxpkg.EstimateTokens` for its internal token estimation (in `Save()` method), or keep a local `estimateTokens` helper to avoid the import
- [ ] Run `go build ./...` to verify no import cycles
- [ ] Run `go vet ./...`

### Import migration checklist

| Old symbol | New symbol | Files affected |
|-----------|-----------|---------------|
| `config.ContextTracker` | `ctxpkg.TokenBudget` | `tui/app.go` |
| `config.NewContextTracker` | `ctxpkg.NewTokenBudgetForModel` | `tui/app.go` |
| `config.GetModelLimit` | `ctxpkg.GetModelLimit` | `config/session.go`, `tui/app.go`, `llm/` |
| `config.ModelLimits` | `ctxpkg.ModelLimits` | any direct map access |
| `config.EstimateTokens` | `ctxpkg.EstimateTokens` | `config/session.go`, `tui/context.go` |
| `config.EstimateSessionTokens` | keep in `config/` as thin wrapper calling `ctxpkg.EstimateTokens` per message | `config/session.go` |
| `config.FormatTokenCount` | `ctxpkg.FormatTokenCount` | `tui/context.go` |
| `config.TruncateToLimit` | removed -- use compaction | `config/session.go` |
| `llm.Summarizer` | `ctxpkg.LLMSummarizer` | `main.go` |
| `llm.ShouldTriggerCompaction` | `budget.ShouldCompactReactive()` | wherever compaction was checked |
| `llm.FormatCompactionResult` | `ctxpkg.FormatCompactionResult` | `tui/app.go` |

**Note on avoiding circular imports:** The new `context/` package must NOT import `config/`, `tui/`, `llm/`, or `agent/`. It defines its own `ChatMessage` and `LLMClient` interface types. The integration layers (`tui/app.go`, `main.go`, `agent/runtime.go`) handle conversion between `tui.ChatMessage` and `context.ChatMessage`.

---

## Task 8: Final verification

- [ ] Run full build and verify no compilation errors:
  ```bash
  go build ./...
  ```
- [ ] Run all tests:
  ```bash
  go test ./cmd/celeste/context/... -v
  ```
- [ ] Run vet:
  ```bash
  go vet ./...
  ```
- [ ] Verify no circular imports:
  ```bash
  go build ./cmd/celeste/context/
  ```
- [ ] Verify old files are deleted:
  ```bash
  test ! -f cmd/celeste/config/context.go && echo "PASS: context.go deleted"
  test ! -f cmd/celeste/config/tokens.go && echo "PASS: tokens.go deleted"
  test ! -f cmd/celeste/llm/summarize.go && echo "PASS: summarize.go deleted"
  ```
- [ ] Manual smoke test: start a TUI session, send several messages, verify token budget displays in status bar
- [ ] Manual smoke test: in agent mode, run a task that produces a large tool output (>32KB), verify it gets capped with disk spill

---

## Dependency graph

```
context/budget.go          (no imports from celeste-cli)
context/limits.go          (no imports from celeste-cli)
context/summarizer.go      (no imports from celeste-cli, defines LLMClient interface)
context/compaction.go      (imports context/summarizer.go types)

tui/app.go                 (imports context/, creates budget + engine)
agent/runtime.go           (imports context/, creates budget + engine)
main.go                    (imports context/, creates LLMSummarizer adapter)
config/session.go          (thin wrapper for EstimateTokens, or imports context/)
```

No circular dependencies. The `context/` package is a leaf package with zero internal imports.

---

## Risk notes

1. **Summarization quality** -- the compacted summary replaces multiple messages. If the LLM produces a poor summary, context is lost permanently. Mitigation: keep at least 4 recent turn pairs, and log the original messages to the session file before compaction.

2. **Token estimation accuracy** -- the 4-chars-per-token heuristic is rough. API usage data (when available) is preferred. The budget tracks both and uses API data when present via `AddTurn()`.

3. **Disk usage from tool result spills** -- large agent sessions could accumulate significant spill files in `~/.celeste/tool-results/`. Consider adding a cleanup routine or TTL-based expiry in a future plan.

4. **Import alias `ctxpkg`** -- Go's stdlib `context` package is used everywhere. The new `cmd/celeste/context` package collides with it. All files importing the new package must use an alias like `ctxpkg`. This is a minor ergonomic cost but avoids renaming the package to something less intuitive.
