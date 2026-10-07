package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
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

	// Decode only a payload with no real line breaks, which is how a
	// double-escaped JSON argument arrives. Content with line breaks keeps
	// its backslash sequences: they are string literals and regexes (#165).
	content, decoded := decodeDoubleEscaped(content)

	targetPath, realPath, err := resolvePathReal(t.workspace, path, true)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}
	guard, err := guardProtectedWrite(targetPath)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}

	// Must-read-before-edit: overwriting or appending to an existing file
	// needs a read in this session (appending blind duplicates content).
	if msg := checkRead(t.tracker, targetPath, path); msg != "" {
		return tools.ToolResult{Error: true, Content: msg}, nil
	}

	// Undo created directories (and a partial new file) on every return
	// below unless the write fully succeeded and verify passed (fix round 6).
	written := false
	defer func() {
		if !written {
			guard.undo()
		}
	}()
	if err := guard.mkdirAll(filepath.Dir(targetPath)); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}
	// Creating directories alone can make a protected name resolve; refuse
	// before writing any bytes.
	if guard.protectedTargetsChanged() {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", protectedError(targetPath))}, nil
	}

	// Checkpoint now that the call validated (path, protected files, stale
	// read, directories), immediately before the write (2.0 F4). A write
	// that fails puts the file back and records nothing.
	var ckpt *checkpoints.Checkpoint
	if t.snapMgr != nil {
		c, err := t.snapMgr.Checkpoint(targetPath, tools.CallIDFromContext(ctx))
		if err != nil {
			return tools.ToolResult{Error: true, Content: fmt.Sprintf("snapshot failed: %s", err)}, nil
		}
		ckpt = c
	}
	fail := func(msg string) (tools.ToolResult, error) {
		return tools.ToolResult{Error: true, Content: rollback(msg, ckpt)}, nil
	}

	var bytesWritten int
	if appendMode {
		// Append is not atomic by nature: it keeps O_APPEND (ruling 5).
		// Use openAppendSecure to prevent TOCTOU on ancestor directories.
		f, err := openAppendSecure(targetPath, 0644)
		if err != nil {
			return fail(err.Error())
		}
		n, err := f.WriteString(content)
		closeErr := f.Close()
		if err != nil {
			return fail(err.Error())
		}
		if closeErr != nil {
			return fail(closeErr.Error())
		}
		bytesWritten = n
	} else {
		if err := writeFileFunc(realPath, []byte(content), 0644); err != nil {
			return fail(err.Error())
		}
		bytesWritten = len(content)
	}
	if err := guard.verify(); err != nil {
		return fail(fmt.Sprintf("path error: %s", err))
	}
	written = true

	// Auto-stamp .grimoire metadata when writing to it
	if filepath.Base(targetPath) == ".grimoire" {
		stampGrimoireMetadata(targetPath, realPath)
	}
	commit(ckpt) // after the stamp: the file as this call leaves it

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
	if decoded {
		result["decoded_escapes"] = true
	}

	return tools.ToolResult{
		Content:  formatResult(result),
		Metadata: result,
	}, nil
}
