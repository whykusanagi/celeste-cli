package tools

import (
	"context"
	"encoding/json"
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
