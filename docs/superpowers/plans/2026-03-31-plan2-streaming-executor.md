# Plan 2: Streaming Tool Executor

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a streaming tool executor that begins running tools as soon as they arrive during LLM streaming, executing concurrency-safe tools in parallel and queuing the rest serially, with cascading failure and interrupt support.

**Architecture:** A new `StreamEvent` type replaces the current batch-oriented `StreamChunk` for tool call delivery, allowing backends to emit partial tool_use blocks as they stream in. A `StreamingToolExecutor` state machine in `cmd/celeste/tools/executor.go` accepts tool calls via `AddTool()`, dispatches them according to concurrency safety, and returns ordered results via `Wait()`. Each backend gains a `SendMessageStreamEvents()` method that emits these new events. The TUI and agent runtime are updated to feed arriving tool calls into the executor instead of waiting for the full response.

**Tech Stack:** Go 1.26, standard library concurrency primitives

**Prerequisite Plans:** Plan 1 (Unified Tool Layer)

---

## Codebase Context

### Current LLM streaming architecture

**`cmd/celeste/llm/interface.go`** defines `LLMBackend` with two methods:
- `SendMessageStream(ctx, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamCallback) error` -- streaming with chunk callback
- `SendMessageSync(ctx, messages []tui.ChatMessage, tools []tui.SkillDefinition) (*ChatCompletionResult, error)` -- blocking

**`cmd/celeste/llm/client.go`** defines:
- `Client` struct wrapping `backend LLMBackend`, holds `registry *skills.Registry` (will be `*tools.Registry` after Plan 1)
- `StreamCallback = func(chunk StreamChunk)`
- `StreamChunk` struct: `Content string`, `IsFirst bool`, `IsFinal bool`, `FinishReason string`, `ToolCalls []ToolCallResult`, `Usage *TokenUsage`
- `ChatCompletionResult` struct: `Content string`, `ToolCalls []ToolCallResult`, `FinishReason string`, `Error error`, `Usage *TokenUsage`
- `ToolCallResult` struct: `ID string`, `Name string`, `Arguments string`
- `TokenUsage` struct: `PromptTokens int`, `CompletionTokens int`, `TotalTokens int`

**`cmd/celeste/llm/stream.go`** defines `StreamState` (chunk accumulation, dump detection) and `SimulatedStream` -- not directly relevant but shares the package.

### Current backends

**`cmd/celeste/llm/backend_openai.go`** (`OpenAIBackend`):
- Uses `github.com/sashabaranov/go-openai` SDK
- `SendMessageStream` iterates `stream.Recv()` in a loop, accumulates `[]openai.ToolCall` by index, sends content deltas via callback, and only includes `ToolCalls` in the final chunk (when `choice.FinishReason != ""`)
- Tool call arguments are concatenated across multiple delta chunks: `toolCalls[idx].Function.Arguments += tc.Function.Arguments`

**`cmd/celeste/llm/backend_google.go`** (`GoogleBackend`):
- Uses `google.golang.org/genai` SDK
- `SendMessageStream` iterates `streamIter` with a range loop, extracts `part.FunctionCall` from each chunk, appends to `[]ToolCallResult`, sends all tool calls in the final callback
- Google sends complete function calls per chunk (not incremental)

**`cmd/celeste/llm/backend_xai.go`** (`XAIBackend`):
- Raw HTTP with SSE parsing (`bufio.Scanner`)
- `SendMessageStream` accumulates `[]xAIToolCall` by matching on `tc.Index`, defers final callback until `[DONE]` sentinel so usage data is captured
- Tool call format matches OpenAI (incremental arguments by index)

### Current tool execution flow

**`cmd/celeste/main.go`** (`TUIClientAdapter.SendMessage`):
1. Calls `client.SendMessageStream()` with a callback that accumulates content
2. Waits for the `IsFinal` chunk
3. Extracts `chunk.ToolCalls` from final chunk
4. Returns `tui.SkillCallBatchMsg{Calls: callRequests, ...}` with ALL tool calls
5. TUI dispatches each call sequentially

**`cmd/celeste/agent/runtime.go`** (`Runner.runState`):
1. Calls `r.client.SendMessageSync()` which blocks until response complete
2. Iterates `result.ToolCalls` serially in a `for` loop
3. Calls `r.executeToolCall(ctx, state, tc)` for each one
4. No parallelism, no streaming

### Post-Plan 1 tool interface (what this plan depends on)

**`cmd/celeste/tools/tool.go`**:
```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error)
    IsConcurrencySafe(input map[string]interface{}) bool
    IsReadOnly() bool
    InterruptBehavior() InterruptBehavior
}

type InterruptBehavior int
const (
    InterruptAbort    InterruptBehavior = iota // Cancel immediately
    InterruptDrain                              // Finish current work, then stop
    InterruptIgnore                             // Cannot be interrupted
)

type ToolResult struct {
    Content  string
    Metadata map[string]interface{}
    Error    error
}

type ProgressEvent struct {
    ToolName string
    Message  string
    Progress float64 // 0.0 to 1.0, -1 for indeterminate
}
```

**`cmd/celeste/tools/registry.go`**:
```go
type Registry struct { ... }
func (r *Registry) Get(name string) (Tool, bool)
func (r *Registry) Execute(ctx context.Context, name string, input map[string]interface{}) (ToolResult, error)
func (r *Registry) ExecuteWithProgress(ctx context.Context, name string, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error)
func (r *Registry) GetToolDefinitions() []map[string]interface{}
func (r *Registry) GetTools(mode RuntimeMode) []Tool

type RuntimeMode int
const (
    ModeTUI    RuntimeMode = iota
    ModeAgent
    ModeReviewer
)
```

### TUI message types

**`cmd/celeste/tui/messages.go`**:
- `SkillCallBatchMsg` struct: `Calls []SkillCallRequest`, `AssistantContent string`, `ToolCalls []ToolCallInfo`
- `SkillCallRequest` struct: `Call FunctionCall`, `ToolCallID string`, `ParseError string`
- `SkillResultMsg` struct: `Name string`, `Result string`, `Err error`, `ToolCallID string`

---

## File Structure

```
cmd/celeste/llm/
├── events.go                          # CREATE - StreamEvent types
├── events_test.go                     # CREATE - StreamEvent tests
├── interface.go                       # MODIFY - add SendMessageStreamEvents to LLMBackend
├── client.go                          # MODIFY - add SendMessageStreamEvents proxy
├── backend_openai.go                  # MODIFY - add SendMessageStreamEvents impl
├── backend_google.go                  # MODIFY - add SendMessageStreamEvents impl
├── backend_xai.go                     # MODIFY - add SendMessageStreamEvents impl

cmd/celeste/tools/
├── executor.go                        # CREATE - StreamingToolExecutor
├── executor_test.go                   # CREATE - executor unit tests

cmd/celeste/
├── main.go                            # MODIFY - use executor in TUIClientAdapter
├── agent/runtime.go                   # MODIFY - use executor in Runner
```

---

## Tasks

### Task 1: StreamEvent Types

**Files:**
- **Create:** `cmd/celeste/llm/events.go`
- **Create:** `cmd/celeste/llm/events_test.go`

#### Step 1: Write failing test

- [ ] Create `cmd/celeste/llm/events_test.go`:

```go
// cmd/celeste/llm/events_test.go
package llm

import (
	"testing"
)

func TestStreamEventType_String(t *testing.T) {
	tests := []struct {
		eventType StreamEventType
		expected  string
	}{
		{EventContentDelta, "ContentDelta"},
		{EventToolUseStart, "ToolUseStart"},
		{EventToolUseInputDelta, "ToolUseInputDelta"},
		{EventToolUseDone, "ToolUseDone"},
		{EventMessageDone, "MessageDone"},
		{StreamEventType(99), "Unknown(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.eventType.String()
			if got != tt.expected {
				t.Errorf("StreamEventType(%d).String() = %q, want %q", tt.eventType, got, tt.expected)
			}
		})
	}
}

func TestStreamEvent_IsToolEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    StreamEvent
		expected bool
	}{
		{"content delta", StreamEvent{Type: EventContentDelta}, false},
		{"tool use start", StreamEvent{Type: EventToolUseStart}, true},
		{"tool use input delta", StreamEvent{Type: EventToolUseInputDelta}, true},
		{"tool use done", StreamEvent{Type: EventToolUseDone}, true},
		{"message done", StreamEvent{Type: EventMessageDone}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.event.IsToolEvent()
			if got != tt.expected {
				t.Errorf("IsToolEvent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStreamEvent_ContentDelta(t *testing.T) {
	event := StreamEvent{
		Type:         EventContentDelta,
		ContentDelta: "Hello, world",
	}

	if event.Type != EventContentDelta {
		t.Errorf("unexpected type: %v", event.Type)
	}
	if event.ContentDelta != "Hello, world" {
		t.Errorf("unexpected content delta: %q", event.ContentDelta)
	}
}

func TestStreamEvent_ToolUseLifecycle(t *testing.T) {
	// Simulate the lifecycle of a tool use event sequence
	events := []StreamEvent{
		{
			Type:     EventToolUseStart,
			ToolUseID: "call_abc123",
			ToolName:  "dev_read_file",
		},
		{
			Type:       EventToolUseInputDelta,
			ToolUseID:  "call_abc123",
			InputDelta: `{"path": "/tmp/`,
		},
		{
			Type:       EventToolUseInputDelta,
			ToolUseID:  "call_abc123",
			InputDelta: `test.txt"}`,
		},
		{
			Type:          EventToolUseDone,
			ToolUseID:     "call_abc123",
			ToolName:      "dev_read_file",
			CompleteInput: `{"path": "/tmp/test.txt"}`,
		},
	}

	// Verify start event
	if events[0].Type != EventToolUseStart {
		t.Error("first event should be ToolUseStart")
	}
	if events[0].ToolUseID != "call_abc123" {
		t.Errorf("unexpected tool use ID: %q", events[0].ToolUseID)
	}
	if events[0].ToolName != "dev_read_file" {
		t.Errorf("unexpected tool name: %q", events[0].ToolName)
	}

	// Verify input deltas
	if events[1].Type != EventToolUseInputDelta {
		t.Error("second event should be ToolUseInputDelta")
	}
	if events[2].InputDelta != `test.txt"}` {
		t.Errorf("unexpected input delta: %q", events[2].InputDelta)
	}

	// Verify done event
	if events[3].Type != EventToolUseDone {
		t.Error("fourth event should be ToolUseDone")
	}
	if events[3].CompleteInput != `{"path": "/tmp/test.txt"}` {
		t.Errorf("unexpected complete input: %q", events[3].CompleteInput)
	}
}

func TestStreamEvent_MessageDone(t *testing.T) {
	event := StreamEvent{
		Type: EventMessageDone,
		Usage: &TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
		FinishReason: "stop",
	}

	if event.Type != EventMessageDone {
		t.Errorf("unexpected type: %v", event.Type)
	}
	if event.Usage == nil {
		t.Fatal("usage should not be nil")
	}
	if event.Usage.TotalTokens != 150 {
		t.Errorf("unexpected total tokens: %d", event.Usage.TotalTokens)
	}
	if event.FinishReason != "stop" {
		t.Errorf("unexpected finish reason: %q", event.FinishReason)
	}
}

func TestStreamEventCallback_Type(t *testing.T) {
	// Verify StreamEventCallback is a valid function type
	var called bool
	var cb StreamEventCallback = func(event StreamEvent) {
		called = true
	}
	cb(StreamEvent{Type: EventContentDelta})
	if !called {
		t.Error("callback was not called")
	}
}

func TestToolUseAccumulator_Basic(t *testing.T) {
	acc := NewToolUseAccumulator()

	// Start a tool use
	acc.HandleEvent(StreamEvent{
		Type:      EventToolUseStart,
		ToolUseID: "call_1",
		ToolName:  "dev_read_file",
	})

	// Feed input deltas
	acc.HandleEvent(StreamEvent{
		Type:       EventToolUseInputDelta,
		ToolUseID:  "call_1",
		InputDelta: `{"path": "`,
	})
	acc.HandleEvent(StreamEvent{
		Type:       EventToolUseInputDelta,
		ToolUseID:  "call_1",
		InputDelta: `/tmp/test.txt"}`,
	})

	// Complete the tool use
	acc.HandleEvent(StreamEvent{
		Type:          EventToolUseDone,
		ToolUseID:     "call_1",
		ToolName:      "dev_read_file",
		CompleteInput: `{"path": "/tmp/test.txt"}`,
	})

	// Check completed calls
	completed := acc.CompletedCalls()
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed call, got %d", len(completed))
	}
	if completed[0].ID != "call_1" {
		t.Errorf("unexpected ID: %q", completed[0].ID)
	}
	if completed[0].Name != "dev_read_file" {
		t.Errorf("unexpected name: %q", completed[0].Name)
	}
	if completed[0].Arguments != `{"path": "/tmp/test.txt"}` {
		t.Errorf("unexpected arguments: %q", completed[0].Arguments)
	}
}

func TestToolUseAccumulator_MultipleTools(t *testing.T) {
	acc := NewToolUseAccumulator()

	// Start two tool uses
	acc.HandleEvent(StreamEvent{Type: EventToolUseStart, ToolUseID: "call_1", ToolName: "dev_read_file"})
	acc.HandleEvent(StreamEvent{Type: EventToolUseStart, ToolUseID: "call_2", ToolName: "dev_list_files"})

	// Feed input deltas interleaved
	acc.HandleEvent(StreamEvent{Type: EventToolUseInputDelta, ToolUseID: "call_1", InputDelta: `{"path": "/tmp/a.txt"}`})
	acc.HandleEvent(StreamEvent{Type: EventToolUseInputDelta, ToolUseID: "call_2", InputDelta: `{"path": "/tmp"}`})

	// Complete both
	acc.HandleEvent(StreamEvent{Type: EventToolUseDone, ToolUseID: "call_1", ToolName: "dev_read_file", CompleteInput: `{"path": "/tmp/a.txt"}`})
	acc.HandleEvent(StreamEvent{Type: EventToolUseDone, ToolUseID: "call_2", ToolName: "dev_list_files", CompleteInput: `{"path": "/tmp"}`})

	completed := acc.CompletedCalls()
	if len(completed) != 2 {
		t.Fatalf("expected 2 completed calls, got %d", len(completed))
	}

	// Verify order matches insertion order
	if completed[0].Name != "dev_read_file" {
		t.Errorf("first call should be dev_read_file, got %q", completed[0].Name)
	}
	if completed[1].Name != "dev_list_files" {
		t.Errorf("second call should be dev_list_files, got %q", completed[1].Name)
	}
}

func TestToolUseAccumulator_PendingCount(t *testing.T) {
	acc := NewToolUseAccumulator()

	if acc.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", acc.PendingCount())
	}

	acc.HandleEvent(StreamEvent{Type: EventToolUseStart, ToolUseID: "call_1", ToolName: "dev_read_file"})
	if acc.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", acc.PendingCount())
	}

	acc.HandleEvent(StreamEvent{Type: EventToolUseDone, ToolUseID: "call_1", ToolName: "dev_read_file", CompleteInput: `{}`})
	if acc.PendingCount() != 0 {
		t.Errorf("expected 0 pending after done, got %d", acc.PendingCount())
	}
}
```

- [ ] Run test, verify it fails to compile:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/llm/ -run TestStreamEvent -count=1
# Expected: compilation error - StreamEventType, StreamEvent, etc. not defined
```

#### Step 2: Implement StreamEvent types

- [ ] Create `cmd/celeste/llm/events.go`:

```go
// Package llm provides the LLM client for Celeste CLI.
// This file defines StreamEvent types for granular streaming events,
// replacing the batch-oriented StreamChunk for tool call delivery.
package llm

import (
	"fmt"
	"sync"
)

// StreamEventType identifies the kind of streaming event.
type StreamEventType int

const (
	// EventContentDelta is emitted when the LLM produces a text content delta.
	EventContentDelta StreamEventType = iota

	// EventToolUseStart is emitted when a new tool_use block begins streaming.
	// Contains the tool call ID and tool name.
	EventToolUseStart

	// EventToolUseInputDelta is emitted as argument JSON accumulates for a tool call.
	// Contains partial JSON input for the tool call identified by ToolUseID.
	EventToolUseInputDelta

	// EventToolUseDone is emitted when a tool call's arguments are fully received.
	// Contains the complete input JSON, ready for execution.
	EventToolUseDone

	// EventMessageDone is emitted when the entire LLM response is complete.
	// Contains usage statistics and finish reason.
	EventMessageDone
)

// String returns a human-readable name for the event type.
func (t StreamEventType) String() string {
	switch t {
	case EventContentDelta:
		return "ContentDelta"
	case EventToolUseStart:
		return "ToolUseStart"
	case EventToolUseInputDelta:
		return "ToolUseInputDelta"
	case EventToolUseDone:
		return "ToolUseDone"
	case EventMessageDone:
		return "MessageDone"
	default:
		return fmt.Sprintf("Unknown(%d)", t)
	}
}

// StreamEvent represents a single granular event during LLM streaming.
// Unlike StreamChunk which batches tool calls into the final chunk,
// StreamEvent delivers tool call information incrementally as it arrives.
type StreamEvent struct {
	// Type identifies which kind of event this is.
	Type StreamEventType

	// ContentDelta contains the text delta (only for EventContentDelta).
	ContentDelta string

	// ToolUseID is the unique identifier for the tool call
	// (set for EventToolUseStart, EventToolUseInputDelta, EventToolUseDone).
	ToolUseID string

	// ToolName is the function name being called
	// (set for EventToolUseStart, EventToolUseDone).
	ToolName string

	// InputDelta contains partial argument JSON
	// (only for EventToolUseInputDelta).
	InputDelta string

	// CompleteInput contains the fully accumulated argument JSON
	// (only for EventToolUseDone).
	CompleteInput string

	// Usage contains token usage statistics (only for EventMessageDone).
	Usage *TokenUsage

	// FinishReason indicates why the response ended (only for EventMessageDone).
	FinishReason string
}

// IsToolEvent returns true if this event relates to a tool use block.
func (e StreamEvent) IsToolEvent() bool {
	switch e.Type {
	case EventToolUseStart, EventToolUseInputDelta, EventToolUseDone:
		return true
	default:
		return false
	}
}

// StreamEventCallback is called for each streaming event.
type StreamEventCallback func(event StreamEvent)

// ToolUseAccumulator tracks in-progress tool uses during streaming,
// collecting input deltas and producing completed ToolCallResult values.
// It is safe for use from a single goroutine (the streaming callback goroutine).
type ToolUseAccumulator struct {
	mu        sync.Mutex
	pending   map[string]*pendingToolUse // keyed by ToolUseID
	order     []string                   // insertion order of ToolUseIDs
	completed []ToolCallResult
}

// pendingToolUse tracks a tool use that is still receiving input deltas.
type pendingToolUse struct {
	id    string
	name  string
	input string // accumulated argument JSON
}

// NewToolUseAccumulator creates a new accumulator for tracking tool uses.
func NewToolUseAccumulator() *ToolUseAccumulator {
	return &ToolUseAccumulator{
		pending: make(map[string]*pendingToolUse),
	}
}

// HandleEvent processes a StreamEvent and updates internal state.
// For EventToolUseStart: creates a new pending tool use.
// For EventToolUseInputDelta: appends to the pending tool's input buffer.
// For EventToolUseDone: moves the tool use to the completed list.
// Other event types are ignored.
func (a *ToolUseAccumulator) HandleEvent(event StreamEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch event.Type {
	case EventToolUseStart:
		a.pending[event.ToolUseID] = &pendingToolUse{
			id:   event.ToolUseID,
			name: event.ToolName,
		}
		a.order = append(a.order, event.ToolUseID)

	case EventToolUseInputDelta:
		if p, ok := a.pending[event.ToolUseID]; ok {
			p.input += event.InputDelta
		}

	case EventToolUseDone:
		// Use CompleteInput from the done event if provided,
		// otherwise fall back to accumulated input.
		input := event.CompleteInput
		if input == "" {
			if p, ok := a.pending[event.ToolUseID]; ok {
				input = p.input
			}
		}

		name := event.ToolName
		if name == "" {
			if p, ok := a.pending[event.ToolUseID]; ok {
				name = p.name
			}
		}

		a.completed = append(a.completed, ToolCallResult{
			ID:        event.ToolUseID,
			Name:      name,
			Arguments: input,
		})
		delete(a.pending, event.ToolUseID)
	}
}

// CompletedCalls returns all tool calls that have finished receiving input,
// in the order they were started.
func (a *ToolUseAccumulator) CompletedCalls() []ToolCallResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := make([]ToolCallResult, len(a.completed))
	copy(result, a.completed)
	return result
}

// PendingCount returns the number of tool uses still accumulating input.
func (a *ToolUseAccumulator) PendingCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pending)
}

// Reset clears all state.
func (a *ToolUseAccumulator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = make(map[string]*pendingToolUse)
	a.order = nil
	a.completed = nil
}
```

- [ ] Run test, verify all pass:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/llm/ -run TestStreamEvent -count=1 -v
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/llm/ -run TestToolUseAccumulator -count=1 -v
# Expected: all tests PASS
```

- [ ] Commit: `feat(llm): add StreamEvent types and ToolUseAccumulator for granular streaming`

---

### Task 2: StreamingToolExecutor Core

**Files:**
- **Create:** `cmd/celeste/tools/executor.go`
- **Create:** `cmd/celeste/tools/executor_test.go`

#### Step 1: Write failing test

- [ ] Create `cmd/celeste/tools/executor_test.go`:

```go
// cmd/celeste/tools/executor_test.go
package tools

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockTool implements Tool for testing the executor.
type mockTool struct {
	name             string
	concurrencySafe  bool
	readOnly         bool
	interruptBehavior InterruptBehavior
	executeFn        func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error)
}

func (m *mockTool) Name() string                          { return m.name }
func (m *mockTool) Description() string                   { return "mock tool" }
func (m *mockTool) Parameters() map[string]interface{}    { return nil }
func (m *mockTool) IsReadOnly() bool                      { return m.readOnly }
func (m *mockTool) InterruptBehavior() InterruptBehavior  { return m.interruptBehavior }
func (m *mockTool) IsConcurrencySafe(input map[string]interface{}) bool {
	return m.concurrencySafe
}
func (m *mockTool) Execute(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, input, progress)
	}
	return ToolResult{Content: fmt.Sprintf("result from %s", m.name)}, nil
}

// newMockRegistry creates a Registry with the given mock tools pre-registered.
func newMockRegistry(tools ...Tool) *Registry {
	r := NewRegistry()
	for _, t := range tools {
		r.Register(t)
	}
	return r
}

func TestStreamingToolExecutor_SingleTool(t *testing.T) {
	tool := &mockTool{name: "read_file", concurrencySafe: true, readOnly: true}
	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutor(registry)

	exec.AddTool("call_1", "read_file", `{"path": "/tmp/test.txt"}`)
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].CallID != "call_1" {
		t.Errorf("unexpected call ID: %q", results[0].CallID)
	}
	if results[0].Result.Error != nil {
		t.Errorf("unexpected error: %v", results[0].Result.Error)
	}
	if results[0].Result.Content != "result from read_file" {
		t.Errorf("unexpected content: %q", results[0].Result.Content)
	}
}

func TestStreamingToolExecutor_ConcurrentTools(t *testing.T) {
	var runningCount atomic.Int32
	var maxConcurrent atomic.Int32

	makeTool := func(name string) *mockTool {
		return &mockTool{
			name:            name,
			concurrencySafe: true,
			readOnly:        true,
			executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
				current := runningCount.Add(1)
				defer runningCount.Add(-1)
				// Track max concurrency
				for {
					old := maxConcurrent.Load()
					if current <= old || maxConcurrent.CompareAndSwap(old, current) {
						break
					}
				}
				time.Sleep(50 * time.Millisecond)
				return ToolResult{Content: fmt.Sprintf("result from %s", name)}, nil
			},
		}
	}

	registry := newMockRegistry(makeTool("read_a"), makeTool("read_b"), makeTool("read_c"))
	exec := NewStreamingToolExecutor(registry)

	exec.AddTool("call_1", "read_a", `{}`)
	exec.AddTool("call_2", "read_b", `{}`)
	exec.AddTool("call_3", "read_c", `{}`)
	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify results are in original call order
	expectedOrder := []string{"call_1", "call_2", "call_3"}
	for i, expected := range expectedOrder {
		if results[i].CallID != expected {
			t.Errorf("result[%d].CallID = %q, want %q", i, results[i].CallID, expected)
		}
	}

	// Verify concurrency happened
	if maxConcurrent.Load() < 2 {
		t.Errorf("expected at least 2 concurrent executions, got %d", maxConcurrent.Load())
	}
}

func TestStreamingToolExecutor_SerialTools(t *testing.T) {
	var executionOrder []string
	var mu sync.Mutex

	makeTool := func(name string) *mockTool {
		return &mockTool{
			name:            name,
			concurrencySafe: false, // NOT concurrency safe
			readOnly:        false,
			executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
				mu.Lock()
				executionOrder = append(executionOrder, name)
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				return ToolResult{Content: fmt.Sprintf("result from %s", name)}, nil
			},
		}
	}

	registry := newMockRegistry(makeTool("write_a"), makeTool("write_b"), makeTool("write_c"))
	exec := NewStreamingToolExecutor(registry)

	exec.AddTool("call_1", "write_a", `{}`)
	exec.AddTool("call_2", "write_b", `{}`)
	exec.AddTool("call_3", "write_c", `{}`)
	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify serial execution order
	mu.Lock()
	defer mu.Unlock()
	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(executionOrder))
	}
	if executionOrder[0] != "write_a" || executionOrder[1] != "write_b" || executionOrder[2] != "write_c" {
		t.Errorf("expected serial order [write_a, write_b, write_c], got %v", executionOrder)
	}

	// Results still in original call order
	for i, expected := range []string{"call_1", "call_2", "call_3"} {
		if results[i].CallID != expected {
			t.Errorf("result[%d].CallID = %q, want %q", i, results[i].CallID, expected)
		}
	}
}

func TestStreamingToolExecutor_MixedConcurrency(t *testing.T) {
	readTool := &mockTool{
		name: "read_file", concurrencySafe: true, readOnly: true,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			time.Sleep(30 * time.Millisecond)
			return ToolResult{Content: "read result"}, nil
		},
	}
	writeTool := &mockTool{
		name: "write_file", concurrencySafe: false, readOnly: false,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			time.Sleep(30 * time.Millisecond)
			return ToolResult{Content: "write result"}, nil
		},
	}

	registry := newMockRegistry(readTool, writeTool)
	exec := NewStreamingToolExecutor(registry)

	exec.AddTool("call_1", "read_file", `{}`)
	exec.AddTool("call_2", "write_file", `{}`)
	exec.AddTool("call_3", "read_file", `{}`)
	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// All results present in order
	if results[0].CallID != "call_1" || results[1].CallID != "call_2" || results[2].CallID != "call_3" {
		t.Errorf("results out of order: %v, %v, %v", results[0].CallID, results[1].CallID, results[2].CallID)
	}

	// No errors
	for i, r := range results {
		if r.Result.Error != nil {
			t.Errorf("result[%d] error: %v", i, r.Result.Error)
		}
	}
}

func TestStreamingToolExecutor_ToolNotFound(t *testing.T) {
	registry := newMockRegistry() // empty registry
	exec := NewStreamingToolExecutor(registry)

	exec.AddTool("call_1", "nonexistent_tool", `{}`)
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != ToolStateFailed {
		t.Errorf("expected Failed state, got %v", results[0].State)
	}
	if results[0].Result.Error == nil {
		t.Error("expected error for missing tool")
	}
}

func TestStreamingToolExecutor_ProgressEvents(t *testing.T) {
	tool := &mockTool{
		name: "slow_tool", concurrencySafe: true, readOnly: true,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			if progress != nil {
				progress <- ProgressEvent{ToolName: "slow_tool", Message: "step 1", Progress: 0.5}
				progress <- ProgressEvent{ToolName: "slow_tool", Message: "step 2", Progress: 1.0}
			}
			return ToolResult{Content: "done"}, nil
		},
	}

	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutor(registry)

	var events []ProgressEvent
	var eventsMu sync.Mutex
	exec.OnProgress(func(event ProgressEvent) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	})

	exec.AddTool("call_1", "slow_tool", `{}`)
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) < 2 {
		t.Errorf("expected at least 2 progress events, got %d", len(events))
	}
}

func TestStreamingToolExecutor_ToolStates(t *testing.T) {
	blocker := make(chan struct{})
	tool := &mockTool{
		name: "blocking_tool", concurrencySafe: true, readOnly: true,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			<-blocker
			return ToolResult{Content: "done"}, nil
		},
	}

	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutor(registry)

	exec.AddTool("call_1", "blocking_tool", `{}`)

	// Give the goroutine time to start
	time.Sleep(20 * time.Millisecond)

	states := exec.States()
	if len(states) != 1 {
		t.Fatalf("expected 1 state entry, got %d", len(states))
	}
	if states["call_1"] != ToolStateExecuting {
		t.Errorf("expected Executing state, got %v", states["call_1"])
	}

	// Unblock
	close(blocker)
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != ToolStateCompleted {
		t.Errorf("expected Completed state, got %v", results[0].State)
	}
}

func TestStreamingToolExecutor_AddToolDuringExecution(t *testing.T) {
	// Verify tools can be added while others are running
	tool := &mockTool{
		name: "fast_tool", concurrencySafe: true, readOnly: true,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			time.Sleep(10 * time.Millisecond)
			return ToolResult{Content: "done"}, nil
		},
	}

	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutor(registry)

	exec.AddTool("call_1", "fast_tool", `{}`)
	time.Sleep(5 * time.Millisecond) // Let first tool start
	exec.AddTool("call_2", "fast_tool", `{}`)

	// Signal no more tools will be added
	exec.Done()
	results := exec.Wait()

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestStreamingToolExecutor_EmptyWait(t *testing.T) {
	registry := newMockRegistry()
	exec := NewStreamingToolExecutor(registry)

	exec.Done()
	results := exec.Wait()

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestToolState_String(t *testing.T) {
	tests := []struct {
		state    ToolState
		expected string
	}{
		{ToolStateQueued, "Queued"},
		{ToolStateExecuting, "Executing"},
		{ToolStateCompleted, "Completed"},
		{ToolStateFailed, "Failed"},
		{ToolStateAborted, "Aborted"},
		{ToolState(99), "Unknown(99)"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("ToolState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}
```

- [ ] Run test, verify it fails to compile:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/ -run TestStreamingToolExecutor -count=1
# Expected: compilation error - NewStreamingToolExecutor, ToolState, etc. not defined
```

#### Step 2: Implement StreamingToolExecutor

- [ ] Create `cmd/celeste/tools/executor.go`:

```go
// Package tools provides the unified tool abstraction for Celeste CLI.
// This file implements StreamingToolExecutor, a state machine that accepts
// tool calls as they arrive during LLM streaming and dispatches them
// according to their concurrency safety.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ToolState represents the lifecycle state of a tool execution.
type ToolState int

const (
	// ToolStateQueued means the tool is waiting to be executed.
	ToolStateQueued ToolState = iota
	// ToolStateExecuting means the tool is currently running.
	ToolStateExecuting
	// ToolStateCompleted means the tool finished successfully.
	ToolStateCompleted
	// ToolStateFailed means the tool finished with an error.
	ToolStateFailed
	// ToolStateAborted means the tool was cancelled (cascading failure or interrupt).
	ToolStateAborted
)

// String returns a human-readable name for the tool state.
func (s ToolState) String() string {
	switch s {
	case ToolStateQueued:
		return "Queued"
	case ToolStateExecuting:
		return "Executing"
	case ToolStateCompleted:
		return "Completed"
	case ToolStateFailed:
		return "Failed"
	case ToolStateAborted:
		return "Aborted"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}

// ExecutorResult holds the result of a single tool execution,
// including the call metadata and final state.
type ExecutorResult struct {
	CallID   string     // The tool call ID from the LLM
	ToolName string     // The tool name
	State    ToolState  // Final state
	Result   ToolResult // Tool output (content and/or error)
}

// toolEntry tracks a single tool call through its lifecycle.
type toolEntry struct {
	callID    string
	toolName  string
	inputJSON string
	state     ToolState
	result    ToolResult
	index     int  // original insertion order
	concurrent bool // whether this tool can run concurrently
}

// StreamingToolExecutor accepts tool calls as they arrive during LLM
// streaming and dispatches them for execution. Concurrency-safe tools
// run in parallel goroutines; non-concurrent tools are queued and
// executed serially. Results are buffered and returned in original
// call order when Wait() is called.
type StreamingToolExecutor struct {
	registry *Registry
	ctx      context.Context
	cancel   context.CancelFunc

	mu          sync.Mutex
	entries     []*toolEntry      // all entries in insertion order
	entryByID   map[string]*toolEntry
	serialQueue chan *toolEntry    // serial tools are fed through this channel
	wg          sync.WaitGroup    // tracks all executing goroutines
	done        chan struct{}      // closed when Done() is called (no more tools)
	doneOnce    sync.Once

	progressFn func(ProgressEvent)
	progressMu sync.RWMutex
}

// NewStreamingToolExecutor creates a new executor bound to the given registry.
// The executor uses a background context; cancel it to abort all running tools.
func NewStreamingToolExecutor(registry *Registry) *StreamingToolExecutor {
	ctx, cancel := context.WithCancel(context.Background())
	e := &StreamingToolExecutor{
		registry:    registry,
		ctx:         ctx,
		cancel:      cancel,
		entryByID:   make(map[string]*toolEntry),
		serialQueue: make(chan *toolEntry, 256),
		done:        make(chan struct{}),
	}
	// Start the serial queue consumer
	e.wg.Add(1)
	go e.serialWorker()
	return e
}

// NewStreamingToolExecutorWithContext creates a new executor with a parent context.
// Cancelling the parent context will abort all running tools.
func NewStreamingToolExecutorWithContext(ctx context.Context, registry *Registry) *StreamingToolExecutor {
	childCtx, cancel := context.WithCancel(ctx)
	e := &StreamingToolExecutor{
		registry:    registry,
		ctx:         childCtx,
		cancel:      cancel,
		entryByID:   make(map[string]*toolEntry),
		serialQueue: make(chan *toolEntry, 256),
		done:        make(chan struct{}),
	}
	e.wg.Add(1)
	go e.serialWorker()
	return e
}

// OnProgress registers a callback for tool progress events.
// Must be called before AddTool. Not safe to call concurrently with AddTool.
func (e *StreamingToolExecutor) OnProgress(fn func(ProgressEvent)) {
	e.progressMu.Lock()
	defer e.progressMu.Unlock()
	e.progressFn = fn
}

// AddTool submits a tool call for execution. This method is non-blocking.
// It parses the input JSON, looks up the tool in the registry, determines
// concurrency safety, and either launches a goroutine or enqueues for serial
// execution. If the tool is not found, the entry is immediately marked Failed.
//
// inputJSON is the raw JSON arguments string from the LLM.
func (e *StreamingToolExecutor) AddTool(callID, toolName, inputJSON string) {
	// Parse input JSON
	var input map[string]interface{}
	if inputJSON != "" && inputJSON != "{}" {
		if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
			// If JSON parsing fails, store the error
			entry := &toolEntry{
				callID:   callID,
				toolName: toolName,
				state:    ToolStateFailed,
				result:   ToolResult{Error: fmt.Errorf("failed to parse tool input JSON: %w", err)},
			}
			e.mu.Lock()
			entry.index = len(e.entries)
			e.entries = append(e.entries, entry)
			e.entryByID[callID] = entry
			e.mu.Unlock()
			return
		}
	}
	if input == nil {
		input = make(map[string]interface{})
	}

	// Look up tool in registry
	tool, ok := e.registry.Get(toolName)
	if !ok {
		entry := &toolEntry{
			callID:   callID,
			toolName: toolName,
			state:    ToolStateFailed,
			result:   ToolResult{Error: fmt.Errorf("tool not found: %s", toolName)},
		}
		e.mu.Lock()
		entry.index = len(e.entries)
		e.entries = append(e.entries, entry)
		e.entryByID[callID] = entry
		e.mu.Unlock()
		return
	}

	// Determine concurrency safety
	concurrent := tool.IsConcurrencySafe(input)

	entry := &toolEntry{
		callID:     callID,
		toolName:   toolName,
		inputJSON:  inputJSON,
		state:      ToolStateQueued,
		concurrent: concurrent,
	}

	e.mu.Lock()
	entry.index = len(e.entries)
	e.entries = append(e.entries, entry)
	e.entryByID[callID] = entry
	e.mu.Unlock()

	if concurrent {
		// Launch in a goroutine immediately
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.executeTool(entry, tool, input)
		}()
	} else {
		// Enqueue for serial execution
		e.serialQueue <- entry
	}
}

// Done signals that no more tool calls will be added.
// Must be called before Wait() will return.
func (e *StreamingToolExecutor) Done() {
	e.doneOnce.Do(func() {
		close(e.done)
	})
}

// Wait blocks until all tool calls have completed and returns results
// in the original call order. Callers must call Done() before or concurrently
// with Wait(), otherwise Wait() will block forever.
func (e *StreamingToolExecutor) Wait() []ExecutorResult {
	// Wait for Done() signal, then close serial queue
	<-e.done
	close(e.serialQueue)

	// Wait for all goroutines to finish
	e.wg.Wait()

	e.mu.Lock()
	defer e.mu.Unlock()

	results := make([]ExecutorResult, len(e.entries))
	for i, entry := range e.entries {
		results[i] = ExecutorResult{
			CallID:   entry.callID,
			ToolName: entry.toolName,
			State:    entry.state,
			Result:   entry.result,
		}
	}
	return results
}

// States returns a snapshot of current tool states.
func (e *StreamingToolExecutor) States() map[string]ToolState {
	e.mu.Lock()
	defer e.mu.Unlock()
	states := make(map[string]ToolState, len(e.entries))
	for _, entry := range e.entries {
		states[entry.callID] = entry.state
	}
	return states
}

// Cancel aborts all running and queued tools.
func (e *StreamingToolExecutor) Cancel() {
	e.cancel()
	e.Done() // ensure Wait() can return
}

// serialWorker processes the serial queue, executing one tool at a time.
func (e *StreamingToolExecutor) serialWorker() {
	defer e.wg.Done()

	for entry := range e.serialQueue {
		// Check if we've been cancelled
		if e.ctx.Err() != nil {
			e.mu.Lock()
			entry.state = ToolStateAborted
			entry.result = ToolResult{Error: e.ctx.Err()}
			e.mu.Unlock()
			continue
		}

		// Look up tool again (should always succeed since AddTool checked)
		tool, ok := e.registry.Get(entry.toolName)
		if !ok {
			e.mu.Lock()
			entry.state = ToolStateFailed
			entry.result = ToolResult{Error: fmt.Errorf("tool not found: %s", entry.toolName)}
			e.mu.Unlock()
			continue
		}

		var input map[string]interface{}
		if entry.inputJSON != "" && entry.inputJSON != "{}" {
			json.Unmarshal([]byte(entry.inputJSON), &input)
		}
		if input == nil {
			input = make(map[string]interface{})
		}

		e.executeTool(entry, tool, input)
	}
}

// executeTool runs a single tool and updates the entry state.
func (e *StreamingToolExecutor) executeTool(entry *toolEntry, tool Tool, input map[string]interface{}) {
	// Transition to Executing
	e.mu.Lock()
	if entry.state == ToolStateAborted {
		e.mu.Unlock()
		return
	}
	entry.state = ToolStateExecuting
	e.mu.Unlock()

	// Set up progress channel
	var progressCh chan ProgressEvent
	e.progressMu.RLock()
	hasFn := e.progressFn != nil
	e.progressMu.RUnlock()

	if hasFn {
		progressCh = make(chan ProgressEvent, 16)
		go func() {
			for event := range progressCh {
				e.progressMu.RLock()
				fn := e.progressFn
				e.progressMu.RUnlock()
				if fn != nil {
					fn(event)
				}
			}
		}()
	}

	// Execute the tool
	result, err := tool.Execute(e.ctx, input, progressCh)

	// Close progress channel
	if progressCh != nil {
		close(progressCh)
	}

	// Update state
	e.mu.Lock()
	if entry.state == ToolStateAborted {
		// Was aborted during execution
		e.mu.Unlock()
		return
	}
	if err != nil {
		entry.state = ToolStateFailed
		result.Error = err
	} else if result.Error != nil {
		entry.state = ToolStateFailed
	} else {
		entry.state = ToolStateCompleted
	}
	entry.result = result
	e.mu.Unlock()
}
```

- [ ] Run test, verify all pass:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/ -run TestStreamingToolExecutor -count=1 -v
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/ -run TestToolState -count=1 -v
# Expected: all tests PASS
```

- [ ] Commit: `feat(tools): add StreamingToolExecutor with concurrent/serial dispatch`

---

### Task 3: Cascading Failure and Interrupt Handling

**Files:**
- **Modify:** `cmd/celeste/tools/executor.go`
- **Modify:** `cmd/celeste/tools/executor_test.go`

#### Step 1: Write failing tests

- [ ] Append to `cmd/celeste/tools/executor_test.go`:

```go
func TestStreamingToolExecutor_CascadingFailure(t *testing.T) {
	failTool := &mockTool{
		name: "fail_tool", concurrencySafe: true, readOnly: true,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			time.Sleep(10 * time.Millisecond)
			return ToolResult{}, fmt.Errorf("intentional failure")
		},
	}
	slowTool := &mockTool{
		name: "slow_tool", concurrencySafe: true, readOnly: true,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			select {
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			case <-time.After(5 * time.Second):
				return ToolResult{Content: "should not reach"}, nil
			}
		},
	}
	queuedTool := &mockTool{
		name: "queued_tool", concurrencySafe: false, readOnly: false,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			return ToolResult{Content: "should not run"}, nil
		},
	}

	registry := newMockRegistry(failTool, slowTool, queuedTool)
	exec := NewStreamingToolExecutor(registry)
	exec.SetCascadeOnFailure(true)

	exec.AddTool("call_1", "fail_tool", `{}`)
	exec.AddTool("call_2", "slow_tool", `{}`)
	exec.AddTool("call_3", "queued_tool", `{}`)
	exec.Done()

	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First tool failed
	if results[0].State != ToolStateFailed {
		t.Errorf("result[0] state = %v, want Failed", results[0].State)
	}

	// Remaining tools should be Aborted or Failed due to context cancellation
	for i := 1; i < len(results); i++ {
		if results[i].State != ToolStateAborted && results[i].State != ToolStateFailed {
			t.Errorf("result[%d] state = %v, want Aborted or Failed", i, results[i].State)
		}
	}
}

func TestStreamingToolExecutor_NoCascadeByDefault(t *testing.T) {
	failTool := &mockTool{
		name: "fail_tool", concurrencySafe: true, readOnly: true,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			return ToolResult{}, fmt.Errorf("intentional failure")
		},
	}
	successTool := &mockTool{
		name: "success_tool", concurrencySafe: true, readOnly: true,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			time.Sleep(30 * time.Millisecond)
			return ToolResult{Content: "success"}, nil
		},
	}

	registry := newMockRegistry(failTool, successTool)
	exec := NewStreamingToolExecutor(registry)
	// cascade is OFF by default

	exec.AddTool("call_1", "fail_tool", `{}`)
	exec.AddTool("call_2", "success_tool", `{}`)
	exec.Done()

	results := exec.Wait()

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First tool failed
	if results[0].State != ToolStateFailed {
		t.Errorf("result[0] state = %v, want Failed", results[0].State)
	}

	// Second tool should still succeed
	if results[1].State != ToolStateCompleted {
		t.Errorf("result[1] state = %v, want Completed", results[1].State)
	}
}

func TestStreamingToolExecutor_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tool := &mockTool{
		name: "interruptable", concurrencySafe: true, readOnly: true,
		interruptBehavior: InterruptAbort,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			select {
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			case <-time.After(5 * time.Second):
				return ToolResult{Content: "should not reach"}, nil
			}
		},
	}

	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutorWithContext(ctx, registry)

	exec.AddTool("call_1", "interruptable", `{}`)

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	exec.Done()
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should be failed or aborted due to context cancellation
	if results[0].State != ToolStateFailed && results[0].State != ToolStateAborted {
		t.Errorf("expected Failed or Aborted state, got %v", results[0].State)
	}
}

func TestStreamingToolExecutor_InterruptBehaviorDrain(t *testing.T) {
	var completed atomic.Bool

	tool := &mockTool{
		name: "draining_tool", concurrencySafe: true, readOnly: true,
		interruptBehavior: InterruptDrain,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			// Simulate work that respects context but completes current unit
			time.Sleep(30 * time.Millisecond)
			completed.Store(true)
			return ToolResult{Content: "drained"}, nil
		},
	}

	registry := newMockRegistry(tool)
	ctx, cancel := context.WithCancel(context.Background())
	exec := NewStreamingToolExecutorWithContext(ctx, registry)

	exec.AddTool("call_1", "draining_tool", `{}`)

	// Cancel almost immediately - tool should still complete since it drains
	time.Sleep(5 * time.Millisecond)
	cancel()

	exec.Done()
	results := exec.Wait()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// A draining tool should complete its current work
	if !completed.Load() {
		t.Error("draining tool should have completed its work")
	}
}

func TestStreamingToolExecutor_AbortAllQueued(t *testing.T) {
	var executedCount atomic.Int32

	tool := &mockTool{
		name: "serial_tool", concurrencySafe: false, readOnly: false,
		executeFn: func(ctx context.Context, input map[string]interface{}, progress chan<- ProgressEvent) (ToolResult, error) {
			executedCount.Add(1)
			time.Sleep(10 * time.Millisecond)
			return ToolResult{Content: "done"}, nil
		},
	}

	registry := newMockRegistry(tool)
	exec := NewStreamingToolExecutor(registry)
	exec.SetCascadeOnFailure(true)

	// Add several serial tools
	exec.AddTool("call_1", "serial_tool", `{}`)
	exec.AddTool("call_2", "serial_tool", `{}`)
	exec.AddTool("call_3", "serial_tool", `{}`)

	// Cancel quickly
	time.Sleep(5 * time.Millisecond)
	exec.Cancel()

	results := exec.Wait()

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Not all 3 should have executed
	if executedCount.Load() == 3 {
		t.Error("expected cancellation to prevent some executions")
	}
}
```

- [ ] Run tests, verify the new tests fail (missing `SetCascadeOnFailure` method):

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/ -run "TestStreamingToolExecutor_Cascad|TestStreamingToolExecutor_NoCascade|TestStreamingToolExecutor_Context|TestStreamingToolExecutor_Interrupt|TestStreamingToolExecutor_Abort" -count=1
# Expected: compilation error - SetCascadeOnFailure not defined
```

#### Step 2: Add cascading failure and interrupt handling

- [ ] Edit `cmd/celeste/tools/executor.go` -- add the `cascadeOnFailure` field to the `StreamingToolExecutor` struct and the `SetCascadeOnFailure` method. Then update `executeTool` to trigger cascade cancellation on failure.

Add to the `StreamingToolExecutor` struct (after the `progressMu` field):

```go
	cascadeOnFailure bool // when true, a failed tool cancels all siblings
```

Add the `SetCascadeOnFailure` method after `OnProgress`:

```go
// SetCascadeOnFailure enables cascading failure mode.
// When enabled, if any tool fails, all queued and executing sibling
// tools are cancelled via context cancellation.
func (e *StreamingToolExecutor) SetCascadeOnFailure(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cascadeOnFailure = enabled
}
```

Update `executeTool` -- after setting `entry.state = ToolStateFailed`, add cascade logic. Replace the state update block at the end of `executeTool` with:

```go
	// Update state
	e.mu.Lock()
	if entry.state == ToolStateAborted {
		// Was aborted during execution
		e.mu.Unlock()
		return
	}
	if err != nil {
		entry.state = ToolStateFailed
		result.Error = err
	} else if result.Error != nil {
		entry.state = ToolStateFailed
	} else {
		entry.state = ToolStateCompleted
	}
	entry.result = result

	// Cascade: if this tool failed and cascade mode is on, cancel everything
	shouldCascade := entry.state == ToolStateFailed && e.cascadeOnFailure
	e.mu.Unlock()

	if shouldCascade {
		// Cancel all siblings
		e.cancel()

		// Mark all queued entries as aborted
		e.mu.Lock()
		for _, other := range e.entries {
			if other.callID != entry.callID && other.state == ToolStateQueued {
				other.state = ToolStateAborted
				other.result = ToolResult{Error: fmt.Errorf("aborted: sibling tool %q failed", entry.toolName)}
			}
		}
		e.mu.Unlock()
	}
```

Also update `executeTool` to handle `InterruptDrain` behavior. Before executing the tool, wrap the context for drain-mode tools. Add this before `result, err := tool.Execute(...)`:

```go
	// For tools with InterruptDrain behavior, create an independent context
	// that ignores parent cancellation, so the tool can finish its current work.
	execCtx := e.ctx
	if tool.InterruptBehavior() == InterruptDrain {
		// Drain tools get a detached context with a generous timeout
		// so they can complete current work even if parent is cancelled.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer drainCancel()
		execCtx = drainCtx
	}
```

And change the Execute call to use `execCtx` instead of `e.ctx`:

```go
	result, err := tool.Execute(execCtx, input, progressCh)
```

- [ ] Run tests, verify all pass:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/ -run TestStreamingToolExecutor -count=1 -v -timeout 30s
# Expected: all tests PASS
```

- [ ] Commit: `feat(tools): add cascading failure and interrupt handling to executor`

---

### Task 4: Update OpenAI Backend with StreamEvent Emission

**Files:**
- **Modify:** `cmd/celeste/llm/interface.go`
- **Modify:** `cmd/celeste/llm/client.go`
- **Modify:** `cmd/celeste/llm/backend_openai.go`

#### Step 1: Add `SendMessageStreamEvents` to the `LLMBackend` interface

- [ ] Edit `cmd/celeste/llm/interface.go` -- add the new method to the `LLMBackend` interface after the existing `SendMessageStream` method:

```go
	// SendMessageStreamEvents sends a message with granular streaming events.
	// Unlike SendMessageStream which batches tool calls into the final chunk,
	// this method emits StreamEvent values as tool_use blocks arrive during
	// streaming. This enables early tool execution before the response completes.
	// Returns error if the request fails.
	SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage,
		tools []tui.SkillDefinition, callback StreamEventCallback) error
```

#### Step 2: Add proxy method to `Client`

- [ ] Edit `cmd/celeste/llm/client.go` -- add the `SendMessageStreamEvents` method after `SendMessageStream`:

```go
// SendMessageStreamEvents sends a message with granular streaming events.
// This delegates to the appropriate backend.
func (c *Client) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	return c.backend.SendMessageStreamEvents(ctx, messages, tools, callback)
}
```

#### Step 3: Implement `SendMessageStreamEvents` in OpenAI backend

- [ ] Edit `cmd/celeste/llm/backend_openai.go` -- add the method. The key difference from `SendMessageStream`: instead of accumulating tool calls and emitting them in the final chunk, emit `EventToolUseStart` when a tool call index first appears, `EventToolUseInputDelta` as argument JSON arrives, and `EventToolUseDone` when `FinishReason` is set or when a new tool call index starts (indicating the previous one is complete).

```go
// SendMessageStreamEvents implements LLMBackend with granular streaming events.
func (b *OpenAIBackend) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	openAIMessages := b.convertMessages(messages)
	openAITools := b.convertTools(tools)

	req := openai.ChatCompletionRequest{
		Model:    b.config.Model,
		Messages: openAIMessages,
		Stream:   true,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
	}

	if len(openAITools) > 0 {
		req.Tools = openAITools
	}

	stream, err := b.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	// Track tool calls being accumulated
	type toolCallState struct {
		id        string
		name      string
		args      string
		started   bool
	}
	var toolCalls []toolCallState
	var usage *TokenUsage

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			// Finalize any incomplete tool calls
			for i := range toolCalls {
				if toolCalls[i].started {
					callback(StreamEvent{
						Type:          EventToolUseDone,
						ToolUseID:     toolCalls[i].id,
						ToolName:      toolCalls[i].name,
						CompleteInput: toolCalls[i].args,
					})
				}
			}
			// Send message done
			callback(StreamEvent{
				Type:         EventMessageDone,
				Usage:        usage,
				FinishReason: "stop",
			})
			return nil
		}
		if err != nil {
			return err
		}

		// Capture usage
		if response.Usage != nil {
			usage = &TokenUsage{
				PromptTokens:     response.Usage.PromptTokens,
				CompletionTokens: response.Usage.CompletionTokens,
				TotalTokens:      response.Usage.TotalTokens,
			}
		}

		for _, choice := range response.Choices {
			// Content delta
			if choice.Delta.Content != "" {
				callback(StreamEvent{
					Type:         EventContentDelta,
					ContentDelta: choice.Delta.Content,
				})
			}

			// Tool calls
			for _, tc := range choice.Delta.ToolCalls {
				if tc.Index != nil {
					idx := *tc.Index

					// Expand slice if needed
					for len(toolCalls) <= idx {
						toolCalls = append(toolCalls, toolCallState{})
					}

					// Accumulate fields
					if tc.ID != "" {
						toolCalls[idx].id = tc.ID
					}
					if tc.Function.Name != "" {
						toolCalls[idx].name = tc.Function.Name
					}

					// Emit ToolUseStart on first appearance
					if !toolCalls[idx].started && (tc.ID != "" || tc.Function.Name != "") {
						toolCalls[idx].started = true
						callback(StreamEvent{
							Type:      EventToolUseStart,
							ToolUseID: toolCalls[idx].id,
							ToolName:  toolCalls[idx].name,
						})
					}

					// Emit input delta
					if tc.Function.Arguments != "" {
						toolCalls[idx].args += tc.Function.Arguments
						callback(StreamEvent{
							Type:       EventToolUseInputDelta,
							ToolUseID:  toolCalls[idx].id,
							InputDelta: tc.Function.Arguments,
						})
					}
				} else {
					// Gemini-style: complete tool call in one chunk
					if tc.ID != "" {
						callback(StreamEvent{
							Type:      EventToolUseStart,
							ToolUseID: tc.ID,
							ToolName:  tc.Function.Name,
						})
						callback(StreamEvent{
							Type:          EventToolUseDone,
							ToolUseID:     tc.ID,
							ToolName:      tc.Function.Name,
							CompleteInput: tc.Function.Arguments,
						})
					}
				}
			}

			// Finish reason
			if choice.FinishReason != "" {
				// Complete any pending tool calls
				for i := range toolCalls {
					if toolCalls[i].started {
						callback(StreamEvent{
							Type:          EventToolUseDone,
							ToolUseID:     toolCalls[i].id,
							ToolName:      toolCalls[i].name,
							CompleteInput: toolCalls[i].args,
						})
						toolCalls[i].started = false // prevent double-emit
					}
				}

				callback(StreamEvent{
					Type:         EventMessageDone,
					Usage:        usage,
					FinishReason: string(choice.FinishReason),
				})
			}
		}
	}
}
```

- [ ] Run existing backend tests to verify no regressions:

```bash
cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/llm/
# Expected: compiles without errors
```

- [ ] Commit: `feat(llm): add SendMessageStreamEvents to OpenAI backend`

---

### Task 5: Update Google Backend with StreamEvent Emission

**Files:**
- **Modify:** `cmd/celeste/llm/backend_google.go`

#### Step 1: Implement `SendMessageStreamEvents`

- [ ] Edit `cmd/celeste/llm/backend_google.go` -- add the method after `SendMessageStream`. Google's GenAI SDK sends complete function calls per chunk (not incremental), so the implementation is simpler: emit `EventToolUseStart` + `EventToolUseDone` together for each function call part.

```go
// SendMessageStreamEvents implements LLMBackend with granular streaming events.
// Google GenAI SDK sends complete function calls per chunk, so ToolUseStart and
// ToolUseDone are emitted together for each function call.
func (b *GoogleBackend) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	contents := b.convertMessagesToGenAI(messages)

	var functionDeclarations []*genai.FunctionDeclaration
	if len(tools) > 0 {
		functionDeclarations = b.convertToolsToGenAI(tools)
	}

	genConfig := &genai.GenerateContentConfig{}
	if b.systemPrompt != "" && !b.config.SkipPersonaPrompt {
		genConfig.SystemInstruction = genai.NewContentFromText(b.systemPrompt, "user")
	}
	if len(functionDeclarations) > 0 {
		genConfig.Tools = []*genai.Tool{
			{FunctionDeclarations: functionDeclarations},
		}
	}

	modelName := b.config.Model
	streamIter := b.client.Models.GenerateContentStream(ctx, modelName, contents, genConfig)

	var lastFinishReason string

	for chunk, err := range streamIter {
		if err != nil {
			return fmt.Errorf("Google AI stream error: %w", err)
		}

		for _, candidate := range chunk.Candidates {
			if candidate.Content != nil {
				text := extractText(candidate.Content)
				if text != "" {
					callback(StreamEvent{
						Type:         EventContentDelta,
						ContentDelta: text,
					})
				}

				// Google sends complete function calls per chunk
				for _, part := range candidate.Content.Parts {
					if part.FunctionCall != nil {
						toolCall := b.convertFunctionCallToResult(part.FunctionCall)

						callback(StreamEvent{
							Type:      EventToolUseStart,
							ToolUseID: toolCall.ID,
							ToolName:  toolCall.Name,
						})
						callback(StreamEvent{
							Type:          EventToolUseDone,
							ToolUseID:     toolCall.ID,
							ToolName:      toolCall.Name,
							CompleteInput: toolCall.Arguments,
						})
					}
				}
			}

			if candidate.FinishReason != "" {
				lastFinishReason = string(candidate.FinishReason)
			}
		}
	}

	callback(StreamEvent{
		Type:         EventMessageDone,
		Usage:        nil, // Google GenAI SDK doesn't provide token usage in streaming yet
		FinishReason: lastFinishReason,
	})

	return nil
}
```

- [ ] Verify compilation:

```bash
cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/llm/
# Expected: compiles without errors
```

- [ ] Commit: `feat(llm): add SendMessageStreamEvents to Google backend`

---

### Task 6: Update xAI Backend with StreamEvent Emission

**Files:**
- **Modify:** `cmd/celeste/llm/backend_xai.go`

#### Step 1: Implement `SendMessageStreamEvents`

- [ ] Edit `cmd/celeste/llm/backend_xai.go` -- add the method after `SendMessageStream`. xAI uses the same incremental format as OpenAI (tool call arguments arrive across multiple SSE chunks by index), so the implementation follows the same pattern as the OpenAI backend.

```go
// SendMessageStreamEvents implements LLMBackend with granular streaming events.
func (b *XAIBackend) SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, callback StreamEventCallback) error {
	xaiMessages := b.convertMessages(messages)
	xaiTools := b.convertTools(tools)

	req := xAIChatCompletionRequest{
		Model:         b.model,
		Messages:      xaiMessages,
		Tools:         xaiTools,
		Stream:        true,
		StreamOptions: &xAIStreamOptions{IncludeUsage: true},
	}

	if b.config.Collections != nil && b.config.Collections.Enabled {
		if len(b.config.Collections.ActiveCollections) > 0 {
			req.CollectionIDs = b.config.Collections.ActiveCollections
		}
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", b.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)

	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("xAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	type toolCallState struct {
		index   int
		id      string
		name    string
		args    string
		started bool
	}

	scanner := bufio.NewScanner(resp.Body)
	var toolCalls []toolCallState
	var usage *TokenUsage

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk xAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			usage = &TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				callback(StreamEvent{
					Type:         EventContentDelta,
					ContentDelta: choice.Delta.Content,
				})
			}

			for _, tc := range choice.Delta.ToolCalls {
				// Find or create slot by index
				var slot *toolCallState
				for i := range toolCalls {
					if toolCalls[i].index == tc.Index {
						slot = &toolCalls[i]
						break
					}
				}
				if slot == nil {
					toolCalls = append(toolCalls, toolCallState{index: tc.Index})
					slot = &toolCalls[len(toolCalls)-1]
				}

				if tc.ID != "" {
					slot.id = tc.ID
				}
				if tc.Function.Name != "" {
					slot.name = tc.Function.Name
				}

				// Emit start on first appearance
				if !slot.started && (tc.ID != "" || tc.Function.Name != "") {
					slot.started = true
					callback(StreamEvent{
						Type:      EventToolUseStart,
						ToolUseID: slot.id,
						ToolName:  slot.name,
					})
				}

				// Input delta
				if tc.Function.Arguments != "" {
					slot.args += tc.Function.Arguments
					callback(StreamEvent{
						Type:       EventToolUseInputDelta,
						ToolUseID:  slot.id,
						InputDelta: tc.Function.Arguments,
					})
				}
			}

			if choice.FinishReason != "" {
				// Complete all pending tool calls
				for i := range toolCalls {
					if toolCalls[i].started {
						callback(StreamEvent{
							Type:          EventToolUseDone,
							ToolUseID:     toolCalls[i].id,
							ToolName:      toolCalls[i].name,
							CompleteInput: toolCalls[i].args,
						})
						toolCalls[i].started = false
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}

	callback(StreamEvent{
		Type:         EventMessageDone,
		Usage:        usage,
		FinishReason: "stop",
	})

	return nil
}
```

- [ ] Verify compilation:

```bash
cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/llm/
# Expected: compiles without errors
```

- [ ] Commit: `feat(llm): add SendMessageStreamEvents to xAI backend`

---

### Task 7: Integration with TUI

**Files:**
- **Modify:** `cmd/celeste/main.go`

#### Step 1: Update TUIClientAdapter.SendMessage to use streaming events + executor

- [ ] Edit `cmd/celeste/main.go`. The existing `TUIClientAdapter.SendMessage` uses `client.SendMessageStream` and batches tool calls from the final chunk. Add an alternative path that uses `client.SendMessageStreamEvents` and feeds tool calls into a `StreamingToolExecutor` as they arrive.

The key changes:
1. Import `cmd/celeste/tools` package
2. Add a `useStreamingExecutor bool` field to `TUIClientAdapter`
3. In `SendMessage`, when `useStreamingExecutor` is true, use `SendMessageStreamEvents` + executor

```go
// Add to TUIClientAdapter struct:
//   toolRegistry     *tools.Registry  // unified tool registry (from Plan 1)
//   useStreamEvents  bool             // use StreamEvent-based execution

// New method using streaming events (called from SendMessage when useStreamEvents is true):
func (a *TUIClientAdapter) sendMessageWithStreamEvents(messages []tui.ChatMessage, skillDefs []tui.SkillDefinition) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var contentBuilder strings.Builder
		executor := tools.NewStreamingToolExecutorWithContext(ctx, a.toolRegistry)
		accumulator := llm.NewToolUseAccumulator()

		err := a.client.SendMessageStreamEvents(ctx, messages, skillDefs, func(event llm.StreamEvent) {
			switch event.Type {
			case llm.EventContentDelta:
				contentBuilder.WriteString(event.ContentDelta)
				// Forward content to TUI for live display (existing StreamChunkMsg path)

			case llm.EventToolUseStart, llm.EventToolUseInputDelta:
				// Accumulate tool call data
				accumulator.HandleEvent(event)

			case llm.EventToolUseDone:
				// Tool call complete - feed to executor immediately
				accumulator.HandleEvent(event)
				executor.AddTool(event.ToolUseID, event.ToolName, event.CompleteInput)

			case llm.EventMessageDone:
				// Signal no more tools will arrive
				executor.Done()
			}
		})

		if err != nil {
			return tui.StreamErrorMsg{Err: err}
		}

		// Wait for all tool executions to complete
		results := executor.Wait()

		if len(results) == 0 {
			// No tool calls - just return the content
			return tui.StreamDoneMsg{
				FullContent: contentBuilder.String(),
			}
		}

		// Convert executor results to SkillCallBatchMsg format
		// (bridge to existing TUI handling until Plan 6 reworks the TUI)
		var callRequests []tui.SkillCallRequest
		var toolCallInfos []tui.ToolCallInfo
		for _, r := range results {
			toolCallInfos = append(toolCallInfos, tui.ToolCallInfo{
				ID:        r.CallID,
				Name:      r.ToolName,
				Arguments: r.Result.Content,
			})
			// Build a SkillCallRequest with pre-computed result
			callRequests = append(callRequests, tui.SkillCallRequest{
				Call: tui.FunctionCall{
					Name:   r.ToolName,
					Result: r.Result.Content,
					Status: "completed",
				},
				ToolCallID: r.CallID,
			})
		}

		return tui.SkillCallBatchMsg{
			Calls:            callRequests,
			AssistantContent: contentBuilder.String(),
			ToolCalls:        toolCallInfos,
		}
	}
}
```

- [ ] Verify compilation:

```bash
cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/
# Expected: compiles without errors
```

- [ ] Run existing TUI tests to verify no regressions:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/ -count=1 -timeout 30s
# Expected: existing tests PASS
```

- [ ] Commit: `feat(tui): add streaming event executor path to TUIClientAdapter`

---

### Task 8: Integration with Agent Runtime

**Files:**
- **Modify:** `cmd/celeste/agent/runtime.go`

#### Step 1: Update the execution loop to use streaming events + executor

- [ ] Edit `cmd/celeste/agent/runtime.go`. The current execution loop in `runState` calls `r.client.SendMessageSync()` which blocks until complete, then iterates tool calls serially. Replace this with `SendMessageStreamEvents` + `StreamingToolExecutor`.

Key changes to the `for state.Turn < state.Options.MaxTurns` loop:

```go
// Replace the existing synchronous call block:
//   result, err := r.client.SendMessageSync(requestCtx, state.Messages, r.client.GetSkills())
// With:

		var contentBuilder strings.Builder
		var toolCallResults []llm.ToolCallResult
		var resultUsage *llm.TokenUsage
		var resultFinishReason string

		executor := tools.NewStreamingToolExecutorWithContext(requestCtx, r.toolRegistry)
		accumulator := llm.NewToolUseAccumulator()

		streamErr := r.client.SendMessageStreamEvents(requestCtx, state.Messages, r.client.GetSkills(), func(event llm.StreamEvent) {
			switch event.Type {
			case llm.EventContentDelta:
				contentBuilder.WriteString(event.ContentDelta)
			case llm.EventToolUseStart, llm.EventToolUseInputDelta:
				accumulator.HandleEvent(event)
			case llm.EventToolUseDone:
				accumulator.HandleEvent(event)
				executor.AddTool(event.ToolUseID, event.ToolName, event.CompleteInput)
			case llm.EventMessageDone:
				resultUsage = event.Usage
				resultFinishReason = event.FinishReason
				executor.Done()
			}
		})

		if streamErr != nil {
			state.Status = StatusFailed
			state.Error = streamErr.Error()
			state.UpdatedAt = time.Now()
			_ = r.store.Save(state)
			r.emitProgress(ProgressError, streamErr.Error(), state.Turn, state.Options.MaxTurns)
			return state, streamErr
		}

		// Wait for all tool executions
		execResults := executor.Wait()

		// Build the equivalent ChatCompletionResult for downstream compatibility
		for _, er := range execResults {
			toolCallResults = append(toolCallResults, llm.ToolCallResult{
				ID:   er.CallID,
				Name: er.ToolName,
			})
		}

		result := &llm.ChatCompletionResult{
			Content:      contentBuilder.String(),
			ToolCalls:    toolCallResults,
			FinishReason: resultFinishReason,
			Usage:        resultUsage,
		}

		// ... rest of the turn processing remains the same ...

		// Replace the serial tool execution loop:
		//   for _, tc := range toolCalls {
		//       toolMsg := r.executeToolCall(ctx, state, tc)
		//       ...
		//   }
		// With:
		for _, er := range execResults {
			content := er.Result.Content
			if er.Result.Error != nil {
				content = fmt.Sprintf("Error: %v", er.Result.Error)
			}
			toolMsg := tui.ChatMessage{
				Role:       "tool",
				Content:    content,
				ToolCallID: er.CallID,
				Name:       er.ToolName,
				Timestamp:  time.Now(),
			}
			state.Messages = append(state.Messages, toolMsg)
			state.ToolCallCount++
		}
```

- [ ] Add `toolRegistry *tools.Registry` field to `Runner` struct and initialize it in `NewRunner`:

```go
// In NewRunner, after creating the skills registry, also create the tools registry:
//   toolRegistry := tools.NewRegistry()
//   // Register tools from the Plan 1 builtin package
//   builtin.RegisterAll(toolRegistry, options.Workspace)

// Add to Runner struct:
//   toolRegistry *tools.Registry
```

- [ ] Verify compilation:

```bash
cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/
# Expected: compiles without errors
```

- [ ] Run agent tests to verify no regressions:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/agent/ -count=1 -timeout 30s
# Expected: existing tests PASS
```

- [ ] Commit: `feat(agent): integrate StreamingToolExecutor into agent runtime`

---

### Task 9: Final Verification

**Files:** None (verification only)

- [ ] Run full test suite:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./... -count=1 -timeout 120s
# Expected: all tests PASS
```

- [ ] Run linter:

```bash
cd /Users/kusanagi/Development/celeste-cli && go vet ./...
# Expected: no issues
```

- [ ] Verify cross-platform build:

```bash
cd /Users/kusanagi/Development/celeste-cli && GOOS=linux GOARCH=amd64 go build ./cmd/celeste/
cd /Users/kusanagi/Development/celeste-cli && GOOS=darwin GOARCH=arm64 go build ./cmd/celeste/
cd /Users/kusanagi/Development/celeste-cli && GOOS=windows GOARCH=amd64 go build ./cmd/celeste/
# Expected: all three build without errors
```

- [ ] Run race detector on executor tests:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/ -run TestStreamingToolExecutor -race -count=1 -timeout 30s
# Expected: no race conditions detected
```

- [ ] Commit: `chore: verify streaming executor builds and passes all tests`
