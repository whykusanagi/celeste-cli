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

// maxPatchLiteralBytes is the ceiling on a single new_string literal. Beyond it,
// a patch is almost certainly a byte-move that belongs in splice_file (deterministic,
// no model-routed payload) rather than a regenerated literal.
const maxPatchLiteralBytes = 16 * 1024

// PatchFileTool performs surgical string replacements in workspace files.
type PatchFileTool struct {
	BaseTool
	workspace string
	tracker   *checkpoints.FileTracker
	snapMgr   *checkpoints.SnapshotManager
}

// NewPatchFileTool creates a PatchFileTool bound to the given workspace directory.
// Optional dependencies can be provided for stale detection and file checkpointing.
func NewPatchFileTool(workspace string, opts ...PatchFileOption) *PatchFileTool {
	t := &PatchFileTool{
		BaseTool: BaseTool{
			ToolName:        "patch_file",
			ToolDescription: "Make surgical edits to a workspace file by replacing exact strings with new content: one old_string/new_string pair, or several in edits[] (applied in order, all or nothing). When old_string is not found exactly, a unique match that differs only in indentation or surrounding whitespace is used and the result shows its diff. Prefer this over write_file when modifying existing files.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative file path inside workspace."
					},
					"old_string": {
						"type": "string",
						"description": "The exact string to find and replace. Must be unique in the file."
					},
					"new_string": {
						"type": "string",
						"description": "The string to replace it with."
					},
					"replace_all": {
						"type": "boolean",
						"description": "Replace every occurrence when true. Defaults to false (fails if old_string appears more than once)."
					},
					"edits": {
						"type": "array",
						"description": "Several edits to the same file, applied in order (each to the result of the previous ones), all or nothing. Use instead of old_string/new_string, not with them. 1-50 edits.",
						"items": {
							"type": "object",
							"properties": {
								"old_string": {"type": "string", "description": "The exact string to find. Must be unique unless replace_all."},
								"new_string": {"type": "string", "description": "The string to replace it with."},
								"replace_all": {"type": "boolean", "description": "Replace every occurrence when true."}
							},
							"required": ["old_string", "new_string"]
						}
					}
				},
				"required": ["path"]
			}`),
			ReadOnly:        false,
			ConcurrencySafe: false,
			RequiredFields:  []string{"path"},
		},
		workspace: workspace,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// PatchFileOption configures optional dependencies for PatchFileTool.
type PatchFileOption func(*PatchFileTool)

// WithPatchFileTracker attaches a FileTracker for stale detection.
func WithPatchFileTracker(ft *checkpoints.FileTracker) PatchFileOption {
	return func(t *PatchFileTool) {
		t.tracker = ft
	}
}

// WithPatchFileSnapshots attaches a SnapshotManager for file checkpointing.
func WithPatchFileSnapshots(sm *checkpoints.SnapshotManager) PatchFileOption {
	return func(t *PatchFileTool) {
		t.snapMgr = sm
	}
}

func (t *PatchFileTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	path := getStringArg(input, "path", "")
	edits, err := parseEdits(input)
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	targetPath, err := resolvePath(t.workspace, path, true)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}
	// patch_file only rewrites an existing file, so the pre-write kernel
	// check is authoritative; a missing file fails at ReadFile below.
	if _, err := guardProtectedWrite(targetPath); err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}

	// Check for stale reads before patching
	if t.tracker != nil {
		if err := t.tracker.CheckStale(targetPath); err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	// Every edit applies in memory, in order; any failure writes nothing.
	patched, outcomes, err := applyEdits(string(data), edits, path)
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	// Checkpoint now that the path resolved and old_string matched,
	// immediately before the write (2.0 F4).
	var ckpt *checkpoints.Checkpoint
	if t.snapMgr != nil {
		c, err := t.snapMgr.Checkpoint(targetPath, tools.CallIDFromContext(ctx))
		if err != nil {
			return tools.ToolResult{Error: true, Content: fmt.Sprintf("snapshot failed: %s", err)}, nil
		}
		ckpt = c
	}
	if err := writeFileFunc(targetPath, []byte(patched), 0644); err != nil {
		return tools.ToolResult{Error: true, Content: rollback(err.Error(), ckpt)}, nil
	}

	// Auto-stamp .grimoire metadata when patching it
	if filepath.Base(targetPath) == ".grimoire" {
		stampGrimoireMetadata(targetPath)
	}
	commit(ckpt) // after the stamp: the file as this call leaves it

	// Record new mtime after patch
	if t.tracker != nil {
		_ = t.tracker.RecordRead(targetPath)
	}

	result := map[string]any{
		"path":      path,
		"workspace": t.workspace,
	}
	if _, multi := input["edits"]; multi && input["edits"] != nil {
		results := make([]map[string]any, len(outcomes))
		for i, out := range outcomes {
			results[i] = outcomeResult(map[string]any{}, edits[i], out)
		}
		result["edits"] = len(edits)
		result["results"] = results
	} else {
		result["replace_all"] = edits[0].All
		outcomeResult(result, edits[0], outcomes[0])
	}

	return tools.ToolResult{
		Content:  formatResult(result),
		Metadata: result,
	}, nil
}

// outcomeResult adds one edit's outcome to m: replacements, fuzzy and its
// diff (ruling 4), and decoded_escapes or the literal-backslash note.
func outcomeResult(m map[string]any, e edit, out editOutcome) map[string]any {
	m["replacements"] = out.Count
	if out.Fuzzy {
		m["fuzzy"] = true
		m["diff"] = out.Diff
	}
	if out.Decoded {
		m["decoded_escapes"] = true
	} else if _, looksEscaped := decodeDoubleEscaped(e.New); looksEscaped {
		m["note"] = `backslash sequences in new_string (e.g. \n) were kept as literal text`
	}
	return m
}
