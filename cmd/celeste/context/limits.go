package ctxmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
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

	// 0700/0600: a spilled tool result may hold secrets (env dumps, API
	// responses, file contents), so keep it readable only by the owner.
	// MkdirAll/WriteFile only apply their mode to a path they create, so an
	// already-existing dir or file (an old binary's spill, or a reused
	// sessionID/toolCallID) is chmod'd explicitly too.
	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return result, false, fmt.Errorf("create tool-results dir: %w", err)
	}
	if err := os.Chmod(sessionDir, 0700); err != nil {
		return result, false, fmt.Errorf("secure tool-results dir: %w", err)
	}

	spillPath := filepath.Join(sessionDir, toolCallID+".txt")
	if err := os.WriteFile(spillPath, []byte(result), 0600); err != nil {
		return result, false, fmt.Errorf("write spill file: %w", err)
	}
	if err := os.Chmod(spillPath, 0600); err != nil {
		return result, false, fmt.Errorf("secure spill file: %w", err)
	}

	// Build the capped preview:
	//   [first N bytes]
	//   --- TRUNCATED (full output: {spillPath}, {total} bytes) ---
	//   [last previewTailBytes bytes]
	totalBytes := len(result)

	// Reserve space for the notice and tail in the budget
	recall := ""
	if id := sessionID + "/" + toolCallID; spillIDPattern.MatchString(id) {
		// #211: the id recall_tool_result takes to page through the file.
		recall = fmt.Sprintf("; recall_tool_result with id %q returns it", id)
	}
	notice := fmt.Sprintf(
		"\n\n--- TRUNCATED (%d bytes total, full output saved to: %s%s) ---\n\n",
		totalBytes, spillPath, recall,
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

// spillIDPattern is a spill file's recall id: <sessionID>/<toolCallID>, both
// plain names (the loop's are), so the id can never leave the spill base.
var spillIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}/[A-Za-z0-9_-]{1,128}$`)

// LoadSpilled returns the full tool result CapToolResult spilled under
// baseDir ("" is ToolResultsBaseDir) for a recall id <sessionID>/<toolCallID>,
// the id its notice names.
func LoadSpilled(baseDir, id string) (string, error) {
	if !spillIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid spilled tool result id %q", id)
	}
	if baseDir == "" {
		var err error
		if baseDir, err = ToolResultsBaseDir(); err != nil {
			return "", err
		}
	}
	session, name, _ := strings.Cut(id, "/")
	b, err := os.ReadFile(filepath.Join(baseDir, session, name+".txt"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no spilled tool result with id %q", id)
		}
		return "", err
	}
	return string(b), nil
}

// snipNotice marks where SnipToolResult cut a result.
const snipNotice = "\n[...snipped %d bytes...]\n"

// SnipToolResult cuts result to at most maxBytes in memory, keeping its head
// and tail around a "[...snipped N bytes...]" marker, without the spill file
// CapToolResult writes (2.0 F3). The loop uses it when the spill file cannot
// be written; sessions and checkpoints use it on tool results loaded from
// disk. Results at or under maxBytes come back unchanged. Cuts fall on UTF-8
// character boundaries. The result is at most maxBytes for any maxBytes of
// 64 or more.
func SnipToolResult(result string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxToolResultBytes
	}
	if len(result) <= maxBytes {
		return result
	}
	// The snipped count is at most len(result), so this reserves enough for
	// the notice whatever the boundaries below turn out to be.
	reserve := len(fmt.Sprintf(snipNotice, len(result)))
	tailLen := min(256, maxBytes/4)
	headLen := max(maxBytes-reserve-tailLen, 0)
	for headLen > 0 && !utf8.RuneStart(result[headLen]) {
		headLen--
	}
	tailStart := len(result) - tailLen
	for tailStart < len(result) && !utf8.RuneStart(result[tailStart]) {
		tailStart++
	}
	return result[:headLen] + fmt.Sprintf(snipNotice, tailStart-headLen) + result[tailStart:]
}
