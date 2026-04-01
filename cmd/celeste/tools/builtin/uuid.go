package builtin

import (
	"context"

	"github.com/google/uuid"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// UUIDTool generates UUID v4 values.
type UUIDTool struct {
	BaseTool
}

// NewUUIDTool creates a UUIDTool.
func NewUUIDTool() *UUIDTool {
	return &UUIDTool{
		BaseTool: BaseTool{
			ToolName:        "generate_uuid",
			ToolDescription: "Generate a random UUID (v4)",
			ToolParameters: mustJSON(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
		},
	}
}

func (t *UUIDTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	id := uuid.New()
	return resultFromMap(map[string]interface{}{
		"uuid": id.String(),
	})
}
