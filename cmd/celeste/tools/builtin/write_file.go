package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// WriteFileTool writes text files to the workspace.
type WriteFileTool struct {
	BaseTool
	workspace string
	tracker   *checkpoints.FileTracker
	snapMgr   *checkpoints.SnapshotManager
}

// NewWriteFileTool creates a WriteFileTool bound to the given workspace directory.
// Optional dependencies can be provided for stale detection and file checkpointing.
func NewWriteFileTool(workspace string, opts ...WriteFileOption) *WriteFileTool {
	t := &WriteFileTool{
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
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// WriteFileOption configures optional dependencies for WriteFileTool.
type WriteFileOption func(*WriteFileTool)

// WithWriteFileTracker attaches a FileTracker for stale detection.
func WithWriteFileTracker(ft *checkpoints.FileTracker) WriteFileOption {
	return func(t *WriteFileTool) {
		t.tracker = ft
	}
}

// WithWriteFileSnapshots attaches a SnapshotManager for file checkpointing.
func WithWriteFileSnapshots(sm *checkpoints.SnapshotManager) WriteFileOption {
	return func(t *WriteFileTool) {
		t.snapMgr = sm
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

	// Check for stale reads before writing
	if t.tracker != nil {
		if err := t.tracker.CheckStale(targetPath); err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
	}

	// Snapshot before writing
	if t.snapMgr != nil {
		if err := t.snapMgr.Snapshot(targetPath); err != nil {
			return tools.ToolResult{Error: true, Content: fmt.Sprintf("snapshot failed: %s", err)}, nil
		}
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

	// Auto-stamp .grimoire metadata when writing to it
	if filepath.Base(targetPath) == ".grimoire" {
		stampGrimoireMetadata(targetPath)
	}

	// Record new mtime after write
	if t.tracker != nil {
		_ = t.tracker.RecordRead(targetPath)
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
