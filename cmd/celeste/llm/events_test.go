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
			Type:      EventToolUseStart,
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
