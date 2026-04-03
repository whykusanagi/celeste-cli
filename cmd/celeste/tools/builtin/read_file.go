package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

const maxReadBytes = 200_000

// ReadFileTool reads text files from the workspace.
type ReadFileTool struct {
	BaseTool
	workspace string
	tracker   *checkpoints.FileTracker
}

// NewReadFileTool creates a ReadFileTool bound to the given workspace directory.
// An optional FileTracker records mtimes after each read for stale detection.
func NewReadFileTool(workspace string, opts ...ReadFileOption) *ReadFileTool {
	t := &ReadFileTool{
		BaseTool: BaseTool{
			ToolName:        "read_file",
			ToolDescription: "Read a text file from workspace. Supports optional line ranges.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative file path inside workspace."
					},
					"start_line": {
						"type": "number",
						"description": "1-based inclusive start line. Defaults to 1."
					},
					"end_line": {
						"type": "number",
						"description": "1-based inclusive end line. Defaults to end-of-file."
					}
				},
				"required": ["path"]
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
			RequiredFields:  []string{"path"},
		},
		workspace: workspace,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// ReadFileOption configures optional dependencies for ReadFileTool.
type ReadFileOption func(*ReadFileTool)

// WithReadFileTracker attaches a FileTracker for stale detection.
func WithReadFileTracker(ft *checkpoints.FileTracker) ReadFileOption {
	return func(t *ReadFileTool) {
		t.tracker = ft
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	path := getStringArg(input, "path", "")
	if path == "" {
		return tools.ToolResult{Error: true, Content: "path is required"}, nil
	}
	startLine := getIntArg(input, "start_line", 1)
	if startLine < 1 {
		startLine = 1
	}
	endLine := getIntArg(input, "end_line", 0)

	targetPath, err := resolvePath(t.workspace, path)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	truncated := false
	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
		truncated = true
	}

	text := string(data)
	lines := strings.Split(text, "\n")
	totalLines := len(lines)

	if endLine <= 0 || endLine > totalLines {
		endLine = totalLines
	}
	if startLine > endLine {
		startLine = endLine
	}

	selected := ""
	if totalLines > 0 {
		selected = strings.Join(lines[startLine-1:endLine], "\n")
	}

	// Record mtime for stale detection
	if t.tracker != nil {
		_ = t.tracker.RecordRead(targetPath)
	}

	result := map[string]any{
		"path":        path,
		"workspace":   t.workspace,
		"start_line":  startLine,
		"end_line":    endLine,
		"total_lines": totalLines,
		"truncated":   truncated,
		"content":     selected,
	}

	return tools.ToolResult{
		Content:  formatResult(result),
		Metadata: result,
	}, nil
}
