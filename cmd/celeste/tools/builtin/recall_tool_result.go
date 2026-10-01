package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/compact"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// recallPageBytes bounds one recall, so restoring a huge result can't blow
// the context straight back up; offset pages through the rest.
const recallPageBytes = 48 * 1024

// RecallToolResultTool returns a tool result that context compaction pruned
// from the conversation (#174), or the full output of one the loop capped and
// spilled to disk when it was recorded (#211).
type RecallToolResultTool struct {
	BaseTool
	store *compact.Store
}

// NewRecallToolResultTool creates the tool over store (nil: the default
// ~/.celeste/tool-results/pruned).
func NewRecallToolResultTool(store *compact.Store) *RecallToolResultTool {
	if store == nil {
		store, _ = compact.DefaultStore()
	}
	return &RecallToolResultTool{
		BaseTool: BaseTool{
			ToolName: "recall_tool_result",
			ToolDescription: "Restore a tool result that was removed from the conversation to save context, or the full output of one that was truncated. " +
				"Use the id given in the placeholder or truncation notice. Long results come back in pages; pass offset to continue.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "The id from the placeholder or truncation notice"},
					"offset": {"type": "integer", "description": "Byte offset to continue from (default 0)"}
				},
				"required": ["id"]
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"id"},
		},
		store: store,
	}
}

func (t *RecallToolResultTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if t.store == nil {
		return tools.ToolResult{Content: "recall_tool_result: no result store is available", Error: true}, nil
	}
	id := getStringArg(input, "id", "")
	body, err := t.store.Load(id)
	if err != nil && strings.Contains(id, "/") {
		// A result the loop spilled when it was recorded: its notice names
		// <session>/<call>, a file beside the pruned store (#211).
		body, err = ctxmgr.LoadSpilled(filepath.Dir(t.store.Dir), id)
	}
	if err != nil {
		return tools.ToolResult{Content: err.Error(), Error: true}, nil
	}
	offset := 0
	if v, ok := input["offset"].(float64); ok && v > 0 {
		offset = int(v)
	}
	if offset >= len(body) {
		return tools.ToolResult{Content: fmt.Sprintf("offset %d is past the end (%d bytes)", offset, len(body)), Error: true}, nil
	}
	end := offset + recallPageBytes
	if end >= len(body) {
		return tools.ToolResult{Content: body[offset:]}, nil
	}
	return tools.ToolResult{Content: fmt.Sprintf("%s\n\n[bytes %d-%d of %d; call recall_tool_result with offset %d for more]",
		body[offset:end], offset, end, len(body), end)}, nil
}
