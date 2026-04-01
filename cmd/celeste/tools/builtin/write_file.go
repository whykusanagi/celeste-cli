package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// WriteFileTool writes text files to the workspace.
type WriteFileTool struct {
	BaseTool
	workspace string
}

// NewWriteFileTool creates a WriteFileTool bound to the given workspace directory.
func NewWriteFileTool(workspace string) *WriteFileTool {
	return &WriteFileTool{
		BaseTool: BaseTool{
			ToolName:        "write_file",
			ToolDescription: "Write text to a workspace file. Creates parent directories automatically.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative file path inside workspace."
					},
					"content": {
						"type": "string",
						"description": "Content to write."
					},
					"append": {
						"type": "boolean",
						"description": "Append instead of overwrite when true."
					}
				},
				"required": ["path", "content"]
			}`),
			ReadOnly:        false,
			ConcurrencySafe: false,
			RequiredFields:  []string{"path", "content"},
		},
		workspace: workspace,
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	path := getStringArg(input, "path", "")
	content := getStringArg(input, "content", "")
	appendMode := getBoolArg(input, "append", false)

	targetPath, err := resolvePath(t.workspace, path)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	var bytesWritten int
	if appendMode {
		f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
		defer f.Close()
		n, err := f.WriteString(content)
		if err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
		bytesWritten = n
	} else {
		if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
		bytesWritten = len(content)
	}

	result := map[string]any{
		"path":          path,
		"workspace":     t.workspace,
		"bytes_written": bytesWritten,
		"append":        appendMode,
	}

	return tools.ToolResult{
		Content:  formatResult(result),
		Metadata: result,
	}, nil
}
