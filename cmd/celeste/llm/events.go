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
