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

// SpliceFileTool moves a region of bytes between files deterministically. The
// model supplies paths + anchors/line-ranges — never the content itself — so a
// large byte-move (relocating a big block, extracting inline CSS to an external
// file) never routes the payload through the model, where it would be slow and a
// silent-corruption vector. It is the deterministic counterpart to patch_file.
type SpliceFileTool struct {
	BaseTool
	workspace string
	tracker   *checkpoints.FileTracker
	snapMgr   *checkpoints.SnapshotManager
}

// NewSpliceFileTool creates a SpliceFileTool bound to the given workspace.
func NewSpliceFileTool(workspace string, opts ...SpliceFileOption) *SpliceFileTool {
	t := &SpliceFileTool{
		BaseTool: BaseTool{
			ToolName:        "splice_file",
			ToolDescription: "Deterministically move or copy a region of bytes between workspace files WITHOUT sending the content through the model. Use this instead of patch_file/write_file for large byte-moves: relocating a block, extracting inline CSS/JS to its own file, or reordering sections. You specify WHERE the bytes are (source + line range or a start/end anchor pair) and WHERE they go (dest + an anchor to insert at, or an anchor pair to replace) — the file contents are copied on disk, never regenerated.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"op": {"type": "string", "enum": ["copy", "move"], "description": "copy leaves source unchanged; move removes the region from source after copying."},
					"source": {"type": "string", "description": "Relative path of the file the region is read from."},
					"dest": {"type": "string", "description": "Relative path of the file the region goes into. Defaults to source (intra-file move). Created if missing."},
					"start_line": {"type": "number", "description": "1-based inclusive start line of the source region. Use with end_line, OR use start_anchor/end_anchor instead."},
					"end_line": {"type": "number", "description": "1-based inclusive end line of the source region."},
					"start_anchor": {"type": "string", "description": "Exact unique string on the first line of the source region. Use with end_anchor as an alternative to line numbers."},
					"end_anchor": {"type": "string", "description": "Exact unique string on the last line of the source region."},
					"dest_anchor": {"type": "string", "description": "Exact unique string in dest to place the region relative to (see position). Omit to append to end of dest."},
					"position": {"type": "string", "enum": ["before", "after"], "description": "Place the region before or after dest_anchor's line. Defaults to after."},
					"dest_replace_start": {"type": "string", "description": "To REPLACE a region in dest: exact string on the first line to overwrite. Use with dest_replace_end."},
					"dest_replace_end": {"type": "string", "description": "Exact string on the last line of the dest region to overwrite."}
				},
				"required": ["op", "source"]
			}`),
			ReadOnly:        false,
			ConcurrencySafe: false,
			RequiredFields:  []string{"op", "source"},
		},
		workspace: workspace,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// SpliceFileOption configures optional dependencies for SpliceFileTool.
type SpliceFileOption func(*SpliceFileTool)

// WithSpliceFileTracker attaches a FileTracker for stale detection.
func WithSpliceFileTracker(ft *checkpoints.FileTracker) SpliceFileOption {
	return func(t *SpliceFileTool) { t.tracker = ft }
}

// WithSpliceFileSnapshots attaches a SnapshotManager for file checkpointing.
func WithSpliceFileSnapshots(sm *checkpoints.SnapshotManager) SpliceFileOption {
	return func(t *SpliceFileTool) { t.snapMgr = sm }
}

func (t *SpliceFileTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return errResult(err.Error()), nil
	}

	op := getStringArg(input, "op", "")
	if op != "copy" && op != "move" {
		return errResult(`op must be "copy" or "move"`), nil
	}
	sourceRel := getStringArg(input, "source", "")
	destRel := getStringArg(input, "dest", sourceRel)

	sourcePath, err := resolvePath(t.workspace, sourceRel)
	if err != nil {
		return errResult(fmt.Sprintf("source path error: %s", err)), nil
	}
	destPath, err := resolvePath(t.workspace, destRel)
	if err != nil {
		return errResult(fmt.Sprintf("dest path error: %s", err)), nil
	}
	sameFile := sourcePath == destPath

	if t.tracker != nil {
		if err := t.tracker.CheckStale(sourcePath); err != nil {
			return errResult(err.Error()), nil
		}
	}

	srcData, err := os.ReadFile(sourcePath)
	if err != nil {
		return errResult(fmt.Sprintf("read source: %s", err)), nil
	}
	source := string(srcData)

	// Resolve the source region [lo, hi) as byte offsets.
	lo, hi, err := resolveSourceRegion(source, input)
	if err != nil {
		return errResult(err.Error()), nil
	}
	region := source[lo:hi]
	if region == "" {
		return errResult("source region is empty"), nil
	}

	// dest content: same file works off the post-removal source for a move so
	// offsets don't shift under us; otherwise read dest (may not exist yet).
	var dest string
	sourceAfter := source
	if op == "move" {
		sourceAfter = source[:lo] + source[hi:]
	}
	if sameFile {
		dest = sourceAfter
	} else {
		if b, rerr := os.ReadFile(destPath); rerr == nil {
			dest = string(b)
		} else if !os.IsNotExist(rerr) {
			return errResult(fmt.Sprintf("read dest: %s", rerr)), nil
		}
	}

	// Splice the region into dest.
	newDest, err := placeRegion(dest, region, input)
	if err != nil {
		return errResult(err.Error()), nil
	}

	// Snapshot before writing (source first, then dest if distinct).
	if t.snapMgr != nil {
		if err := t.snapMgr.Snapshot(sourcePath); err != nil {
			return errResult(fmt.Sprintf("snapshot source: %s", err)), nil
		}
		if !sameFile {
			if err := t.snapMgr.Snapshot(destPath); err != nil {
				return errResult(fmt.Sprintf("snapshot dest: %s", err)), nil
			}
		}
	}

	// Write. For a same-file move, dest already reflects the removal.
	if err := os.WriteFile(destPath, []byte(newDest), 0644); err != nil {
		return errResult(fmt.Sprintf("write dest: %s", err)), nil
	}
	if op == "move" && !sameFile {
		if err := os.WriteFile(sourcePath, []byte(sourceAfter), 0644); err != nil {
			return errResult(fmt.Sprintf("write source: %s", err)), nil
		}
	}

	// Anchor verification: the region must now be present in dest, and (for a
	// cross-file move) absent from source. Byte-exact, no model involvement.
	if !strings.Contains(newDest, region) {
		return errResult("verification failed: spliced region not found in dest after write"), nil
	}
	if op == "move" && !sameFile && strings.Contains(sourceAfter, region) {
		return errResult("verification failed: region still present in source after move"), nil
	}

	if t.tracker != nil {
		_ = t.tracker.RecordRead(sourcePath)
		if !sameFile {
			_ = t.tracker.RecordRead(destPath)
		}
	}

	result := map[string]any{
		"op":                 op,
		"source":             sourceRel,
		"dest":               destRel,
		"bytes_moved":        len(region),
		"source_bytes":       len(sourceAfter),
		"dest_bytes":         len(newDest),
		"routed_through_llm": false,
	}
	return tools.ToolResult{Content: formatResult(result), Metadata: result}, nil
}

// resolveSourceRegion returns the byte offsets [lo, hi) of the source region,
// from either a line range or a start/end anchor pair. Anchor regions are
// expanded to whole lines so a move leaves clean boundaries.
func resolveSourceRegion(source string, input map[string]any) (int, int, error) {
	startAnchor := unescape(getStringArg(input, "start_anchor", ""))
	endAnchor := unescape(getStringArg(input, "end_anchor", ""))
	if startAnchor != "" || endAnchor != "" {
		if startAnchor == "" || endAnchor == "" {
			return 0, 0, fmt.Errorf("both start_anchor and end_anchor are required for anchor mode")
		}
		lo, err := uniqueIndex(source, startAnchor, "start_anchor")
		if err != nil {
			return 0, 0, err
		}
		endIdx, err := uniqueIndex(source, endAnchor, "end_anchor")
		if err != nil {
			return 0, 0, err
		}
		hi := endIdx + len(endAnchor)
		if hi <= lo {
			return 0, 0, fmt.Errorf("end_anchor occurs before start_anchor")
		}
		lo, hi = expandToLines(source, lo, hi)
		return lo, hi, nil
	}

	// Line range.
	startLine := getIntArg(input, "start_line", 0)
	endLine := getIntArg(input, "end_line", 0)
	if startLine < 1 || endLine < startLine {
		return 0, 0, fmt.Errorf("provide a valid line range (start_line >= 1, end_line >= start_line) or a start_anchor/end_anchor pair")
	}
	lo, hi, ok := lineRangeOffsets(source, startLine, endLine)
	if !ok {
		return 0, 0, fmt.Errorf("line range %d-%d is outside the source file", startLine, endLine)
	}
	return lo, hi, nil
}

// placeRegion inserts region into dest per the dest_* args: replace between a
// dest anchor pair, insert relative to a single dest anchor, or append.
func placeRegion(dest, region string, input map[string]any) (string, error) {
	repStart := unescape(getStringArg(input, "dest_replace_start", ""))
	repEnd := unescape(getStringArg(input, "dest_replace_end", ""))
	if repStart != "" || repEnd != "" {
		if repStart == "" || repEnd == "" {
			return "", fmt.Errorf("both dest_replace_start and dest_replace_end are required to replace a dest region")
		}
		lo, err := uniqueIndex(dest, repStart, "dest_replace_start")
		if err != nil {
			return "", err
		}
		endIdx, err := uniqueIndex(dest, repEnd, "dest_replace_end")
		if err != nil {
			return "", err
		}
		hi := endIdx + len(repEnd)
		if hi <= lo {
			return "", fmt.Errorf("dest_replace_end occurs before dest_replace_start")
		}
		lo, hi = expandToLines(dest, lo, hi)
		return dest[:lo] + ensureTrailingNewline(region) + dest[hi:], nil
	}

	anchor := unescape(getStringArg(input, "dest_anchor", ""))
	if anchor == "" {
		// Append, guaranteeing a newline separator.
		if dest == "" {
			return region, nil
		}
		return ensureTrailingNewline(dest) + region, nil
	}
	idx, err := uniqueIndex(dest, anchor, "dest_anchor")
	if err != nil {
		return "", err
	}
	position := getStringArg(input, "position", "after")
	lineLo, lineHi := expandToLines(dest, idx, idx+len(anchor))
	switch position {
	case "before":
		return dest[:lineLo] + ensureTrailingNewline(region) + dest[lineLo:], nil
	case "after":
		return dest[:lineHi] + ensureTrailingNewline(region) + dest[lineHi:], nil
	default:
		return "", fmt.Errorf(`position must be "before" or "after"`)
	}
}

// uniqueIndex returns the byte offset of needle in haystack, erroring if it is
// missing or ambiguous (appears more than once) — so anchors are unambiguous.
func uniqueIndex(haystack, needle, label string) (int, error) {
	n := strings.Count(haystack, needle)
	if n == 0 {
		return 0, fmt.Errorf("%s not found: %q", label, truncateAnchor(needle))
	}
	if n > 1 {
		return 0, fmt.Errorf("%s appears %d times (must be unique): %q", label, n, truncateAnchor(needle))
	}
	return strings.Index(haystack, needle), nil
}

// lineRangeOffsets returns byte offsets [lo, hi) spanning 1-based inclusive lines
// [start, end], where hi includes the trailing newline of `end` (or EOF).
func lineRangeOffsets(s string, start, end int) (int, int, bool) {
	line := 1
	lo := -1
	if start == 1 {
		lo = 0
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line++
			if line == start {
				lo = i + 1
			}
			if line == end+1 {
				if lo < 0 {
					return 0, 0, false
				}
				return lo, i + 1, true
			}
		}
	}
	// end runs to EOF (no trailing newline on the last line).
	if lo >= 0 && end >= line {
		return lo, len(s), true
	}
	return 0, 0, false
}

// expandToLines widens [lo, hi) to whole-line boundaries: lo back to its line
// start, hi forward past the next newline (inclusive), so removals stay clean.
func expandToLines(s string, lo, hi int) (int, int) {
	if nl := strings.LastIndexByte(s[:lo], '\n'); nl >= 0 {
		lo = nl + 1
	} else {
		lo = 0
	}
	if hi < len(s) {
		if nl := strings.IndexByte(s[hi:], '\n'); nl >= 0 {
			hi = hi + nl + 1
		} else {
			hi = len(s)
		}
	}
	return lo, hi
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	return s
}

func truncateAnchor(s string) string {
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

func errResult(msg string) tools.ToolResult {
	return tools.ToolResult{Error: true, Content: msg}
}
