package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// PatchFileTool performs surgical string replacements in workspace files.
type PatchFileTool struct {
	BaseTool
	workspace string
}

// NewPatchFileTool creates a PatchFileTool bound to the given workspace directory.
func NewPatchFileTool(workspace string) *PatchFileTool {
	return &PatchFileTool{
		BaseTool: BaseTool{
			ToolName:        "patch_file",
			ToolDescription: "Make a surgical edit to a workspace file by replacing an exact string with new content. Prefer this over write_file when modifying existing files.",
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
					}
				},
				"required": ["path", "old_string", "new_string"]
			}`),
			ReadOnly:        false,
			ConcurrencySafe: false,
			RequiredFields:  []string{"path", "old_string", "new_string"},
		},
		workspace: workspace,
	}
}

func (t *PatchFileTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	path := getStringArg(input, "path", "")
	oldString := getStringArg(input, "old_string", "")
	newString := getStringArg(input, "new_string", "")
	replaceAll := getBoolArg(input, "replace_all", false)

	targetPath, err := resolvePath(t.workspace, path)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}
	original := string(data)

	count := strings.Count(original, oldString)
	if count == 0 {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("old_string not found in %s", path)}, nil
	}
	if !replaceAll && count > 1 {
		return tools.ToolResult{
			Error:   true,
			Content: fmt.Sprintf("old_string appears %d times in %s — set replace_all:true or make it more specific", count, path),
		}, nil
	}

	var patched string
	if replaceAll {
		patched = strings.ReplaceAll(original, oldString, newString)
	} else {
		patched = strings.Replace(original, oldString, newString, 1)
	}

	if err := os.WriteFile(targetPath, []byte(patched), 0644); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	result := map[string]any{
		"path":         path,
		"workspace":    t.workspace,
		"replacements": count,
		"replace_all":  replaceAll,
	}

	return tools.ToolResult{
		Content:  formatResult(result),
		Metadata: result,
	}, nil
}
