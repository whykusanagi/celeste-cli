package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// ListFilesTool lists files and directories inside the workspace.
type ListFilesTool struct {
	BaseTool
	workspace string
}

// NewListFilesTool creates a ListFilesTool bound to the given workspace directory.
func NewListFilesTool(workspace string) *ListFilesTool {
	return &ListFilesTool{
		BaseTool: BaseTool{
			ToolName:        "list_files",
			ToolDescription: "List files/directories inside the configured workspace. Use this before reading or editing files.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative directory path inside workspace. Defaults to '.'"
					},
					"recursive": {
						"type": "boolean",
						"description": "Recursively walk subdirectories when true."
					},
					"max_entries": {
						"type": "number",
						"description": "Maximum entries to return. Default 200."
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
}

func (t *ListFilesTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	path := getStringArg(input, "path", ".")
	recursive := getBoolArg(input, "recursive", false)
	maxEntries := getIntArg(input, "max_entries", 200)
	if maxEntries <= 0 {
		maxEntries = 200
	}
	if maxEntries > 1000 {
		maxEntries = 1000
	}

	targetPath, err := resolvePath(t.workspace, path)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}

	entries := make([]map[string]any, 0, maxEntries)
	truncated := false

	if !recursive {
		dirs, err := os.ReadDir(targetPath)
		if err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
		for _, entry := range dirs {
			if len(entries) >= maxEntries {
				truncated = true
				break
			}
			info, _ := entry.Info()
			rel, _ := filepath.Rel(t.workspace, filepath.Join(targetPath, entry.Name()))
			entries = append(entries, map[string]any{
				"path":   rel,
				"name":   entry.Name(),
				"is_dir": entry.IsDir(),
				"size":   fileSize(info),
			})
		}
	} else {
		err = filepath.WalkDir(targetPath, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if p == targetPath {
				return nil
			}
			if len(entries) >= maxEntries {
				truncated = true
				return fs.SkipAll
			}
			info, _ := d.Info()
			rel, _ := filepath.Rel(t.workspace, p)
			entries = append(entries, map[string]any{
				"path":   rel,
				"name":   d.Name(),
				"is_dir": d.IsDir(),
				"size":   fileSize(info),
			})
			return nil
		})
		if err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
	}

	result := map[string]any{
		"workspace": t.workspace,
		"path":      path,
		"entries":   entries,
		"count":     len(entries),
		"truncated": truncated,
	}

	return tools.ToolResult{
		Content:  formatResult(result),
		Metadata: result,
	}, nil
}
