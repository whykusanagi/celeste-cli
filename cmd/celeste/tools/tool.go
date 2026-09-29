package tools

import (
	"context"
	"encoding/json"
	"time"
)

// Tool defines the interface that all tools must implement.
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	IsConcurrencySafe(input map[string]any) bool
	IsReadOnly() bool
	ValidateInput(input map[string]any) error
	Execute(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error)
	InterruptBehavior() InterruptBehavior
}

// Timeouter is implemented by tools that need their own execution timeout
// (a long build, a subagent, TTS). Zero means "use the caller's default".
type Timeouter interface {
	Timeout() time.Duration
}

// TimeoutFor returns t's own timeout when it has one, else def.
func TimeoutFor(t Tool, def time.Duration) time.Duration {
	if tt, ok := t.(Timeouter); ok {
		if d := tt.Timeout(); d > 0 {
			return d
		}
	}
	return def
}

// InterruptBehavior defines how a tool responds to cancellation signals.
type InterruptBehavior int

const (
	// InterruptCancel means the tool should be cancelled immediately.
	InterruptCancel InterruptBehavior = iota
	// InterruptBlock means the tool should block until completion.
	InterruptBlock
)

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	Content  string         `json:"content"`
	Error    bool           `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ProgressEvent represents a progress update from a running tool.
type ProgressEvent struct {
	ToolName string  `json:"tool_name"`
	Message  string  `json:"message"`
	Percent  float64 `json:"percent"` // -1 for indeterminate
}

// RuntimeMode represents the execution mode of the CLI.
type RuntimeMode int

const (
	ModeChat RuntimeMode = iota
	ModeClaw
	ModeAgent
	ModeOrchestrator
)
