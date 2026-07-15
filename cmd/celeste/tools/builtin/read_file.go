package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// maxReadBytes is the hard ceiling on how much of a file we pull into memory.
const maxReadBytes = 512_000

// defaultMaxResultBytes bounds the *returned* content, well under the 128 KiB
// tool-result history cap (context.DefaultMaxToolResultBytes) so a read never
// produces an oversized, silently-capped tool message.
const defaultMaxResultBytes = 48 * 1024

// ReadFileTool reads text files from the workspace.
type ReadFileTool struct {
	BaseTool
	workspace      string
	tracker        *checkpoints.FileTracker
	maxResultBytes int
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
		workspace:      workspace,
		maxResultBytes: defaultMaxResultBytes,
	}
	for _, opt := range opts {
		opt(t)
	}
	if t.maxResultBytes <= 0 {
		t.maxResultBytes = defaultMaxResultBytes
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

// WithMaxResultBytes overrides the returned-content byte budget (mainly for tests).
func WithMaxResultBytes(n int) ReadFileOption {
	return func(t *ReadFileTool) {
		t.maxResultBytes = n
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

	// Check for image files and handle them with base64 encoding
	ext := strings.ToLower(filepath.Ext(targetPath))
	if isImageExtension(ext) {
		return t.readImageFile(targetPath, path, ext)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	totalBytes := len(data)
	readTruncated := false
	if len(data) > maxReadBytes {
		data = data[:lineAlignedCut(data, maxReadBytes)]
		readTruncated = true
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

	var selectedLines []string
	if totalLines > 0 {
		selectedLines = lines[startLine-1 : endLine]
	}

	// Bound the returned content to the byte budget, cutting on a line boundary,
	// so the line range can't produce an unbounded payload on minified files.
	content, returnedEndLine, budgetTruncated := budgetLines(selectedLines, startLine, t.maxResultBytes)
	truncated := readTruncated || budgetTruncated

	// Record mtime for stale detection
	if t.tracker != nil {
		_ = t.tracker.RecordRead(targetPath)
	}

	result := map[string]any{
		"path":           path,
		"workspace":      t.workspace,
		"start_line":     startLine,
		"end_line":       returnedEndLine,
		"total_lines":    totalLines,
		"total_bytes":    totalBytes,
		"returned_bytes": len(content),
		"truncated":      truncated,
		"content":        content,
	}
	if truncated {
		result["next_offset_line"] = returnedEndLine + 1
		result["hint"] = "File is large or generated; only part was returned. Re-run read_file with start_line=next_offset_line to page, or use `search` for targeted lookups."
	}

	return tools.ToolResult{
		Content:  formatResult(result),
		Metadata: result,
	}, nil
}

// lineAlignedCut returns the largest index <= limit that ends on a newline, so
// a raw read cut never splits a line. Falls back to limit when there is no
// newline in the head (a single oversized line).
func lineAlignedCut(data []byte, limit int) int {
	if limit >= len(data) {
		return len(data)
	}
	if nl := bytes.LastIndexByte(data[:limit], '\n'); nl >= 0 {
		return nl + 1
	}
	return limit
}

// budgetLines joins as many whole lines as fit within maxBytes, cutting on a
// line boundary. It returns the joined content, the 1-based number of the last
// line included, and whether any lines were dropped. If even the first line
// exceeds the budget (a single minified line), it returns a byte-bounded head
// of that line — the only option — and marks it truncated.
func budgetLines(lines []string, startLineNum, maxBytes int) (content string, lastLine int, truncated bool) {
	if len(lines) == 0 {
		return "", startLineNum - 1, false
	}
	var b strings.Builder
	lastLine = startLineNum - 1
	for i, ln := range lines {
		add := ln
		if i > 0 {
			add = "\n" + ln
		}
		if b.Len()+len(add) > maxBytes {
			if b.Len() == 0 { // first line alone exceeds the budget
				head := ln
				if len(head) > maxBytes {
					head = head[:maxBytes]
				}
				return head, startLineNum, true
			}
			return b.String(), lastLine, true
		}
		b.WriteString(add)
		lastLine = startLineNum + i
	}
	return b.String(), lastLine, false
}

// maxImageBytes limits image file reads to 10 MB.
const maxImageBytes = 10 * 1024 * 1024

// imageExtensions lists file extensions treated as images.
var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
}

// isImageExtension reports whether ext (with leading dot, lowercase) is an image extension.
func isImageExtension(ext string) bool {
	return imageExtensions[ext]
}

// readImageFile reads an image file and returns a ToolResult with base64-encoded
// content in the Metadata map, enabling downstream consumers (LLM client / TUI
// adapter) to convert it into provider-specific image content blocks.
func (t *ReadFileTool) readImageFile(targetPath, relPath, ext string) (tools.ToolResult, error) {
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	if len(data) > maxImageBytes {
		return tools.ToolResult{
			Error:   true,
			Content: fmt.Sprintf("image file too large: %d bytes (max %d)", len(data), maxImageBytes),
		}, nil
	}

	base64Data := base64.StdEncoding.EncodeToString(data)
	format := ext[1:] // strip leading dot: ".png" -> "png"

	// Record mtime for stale detection
	if t.tracker != nil {
		_ = t.tracker.RecordRead(targetPath)
	}

	return tools.ToolResult{
		Content: fmt.Sprintf("Image file: %s (%s, %d bytes)", relPath, format, len(data)),
		Metadata: map[string]any{
			"type":     "image",
			"format":   format,
			"base64":   base64Data,
			"filename": filepath.Base(targetPath),
		},
	}, nil
}
