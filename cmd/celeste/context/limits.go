package ctxmgr

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DefaultMaxToolResultBytes is the maximum size in bytes for a single tool
	// result before it gets capped and spilled to disk. 32KB.
	DefaultMaxToolResultBytes = 128 * 1024

	// previewTailBytes controls how many bytes from the end of the result are
	// included in the preview (so the model sees both the beginning and end).
	previewTailBytes = 512
)

// ToolResultsBaseDir returns the base directory for spilled tool results.
// Default: ~/.celeste/tool-results
func ToolResultsBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".celeste", "tool-results"), nil
}

// CapToolResult checks whether a tool result exceeds maxBytes. If it does, the
// full result is written to disk at:
//
//	{baseDir}/{sessionID}/{toolCallID}.txt
//
// and a truncated preview is returned containing the first portion, a notice
// with the file path, and the last previewTailBytes of the result.
//
// If baseDir is empty, ToolResultsBaseDir() is used.
//
// Returns:
//   - capped: the (possibly truncated) result string to send to the model
//   - wasCapped: true if the result was truncated
//   - err: any I/O error from writing the spill file
func CapToolResult(result string, maxBytes int, sessionID, toolCallID, baseDir string) (capped string, wasCapped bool, err error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxToolResultBytes
	}

	if len(result) <= maxBytes {
		return result, false, nil
	}

	// Determine spill directory
	if baseDir == "" {
		baseDir, err = ToolResultsBaseDir()
		if err != nil {
			return result, false, err
		}
	}

	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return result, false, fmt.Errorf("create tool-results dir: %w", err)
	}

	spillPath := filepath.Join(sessionDir, toolCallID+".txt")
	if err := os.WriteFile(spillPath, []byte(result), 0644); err != nil {
		return result, false, fmt.Errorf("write spill file: %w", err)
	}

	// Build the capped preview:
	//   [first N bytes]
	//   --- TRUNCATED (full output: {spillPath}, {total} bytes) ---
	//   [last previewTailBytes bytes]
	totalBytes := len(result)

	// Reserve space for the notice and tail in the budget
	notice := fmt.Sprintf(
		"\n\n--- TRUNCATED (%d bytes total, full output saved to: %s) ---\n\n",
		totalBytes, spillPath,
	)
	noticeLen := len(notice)
	tailLen := previewTailBytes
	if tailLen > totalBytes {
		tailLen = totalBytes
	}

	headLen := maxBytes - noticeLen - tailLen
	if headLen < 256 {
		headLen = 256 // Ensure a minimum head size
	}
	if headLen > totalBytes {
		headLen = totalBytes
	}

	tail := result[totalBytes-tailLen:]
	head := result[:headLen]

	capped = head + notice + tail
	return capped, true, nil
}
