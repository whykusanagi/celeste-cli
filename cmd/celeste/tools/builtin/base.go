package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// BaseTool provides a reusable base implementation of the tools.Tool interface.
// Concrete tools can embed BaseTool and override Execute.
type BaseTool struct {
	ToolName        string
	ToolDescription string
	ToolParameters  json.RawMessage
	ReadOnly        bool
	ConcurrencySafe bool
	Interrupt       tools.InterruptBehavior
	RequiredFields  []string
}

func (b *BaseTool) Name() string                                { return b.ToolName }
func (b *BaseTool) Description() string                         { return b.ToolDescription }
func (b *BaseTool) Parameters() json.RawMessage                 { return b.ToolParameters }
func (b *BaseTool) IsConcurrencySafe(input map[string]any) bool { return b.ConcurrencySafe }
func (b *BaseTool) IsReadOnly() bool                            { return b.ReadOnly }
func (b *BaseTool) InterruptBehavior() tools.InterruptBehavior  { return b.Interrupt }

// ValidateInput checks that all required fields are present and non-empty.
func (b *BaseTool) ValidateInput(input map[string]any) error {
	for _, field := range b.RequiredFields {
		val, ok := input[field]
		if !ok || val == nil || val == "" {
			return fmt.Errorf("required field '%s' is missing", field)
		}
	}
	return nil
}

// Execute is a default implementation that returns an error.
// Concrete tools should override this method.
func (b *BaseTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{}, fmt.Errorf("Execute not implemented for tool '%s'", b.ToolName)
}
