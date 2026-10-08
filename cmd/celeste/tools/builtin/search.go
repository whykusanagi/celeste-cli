package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// SearchTool searches for text patterns in workspace files.
type SearchTool struct {
	BaseTool
	workspace string
}

// NewSearchTool creates a SearchTool bound to the given workspace directory.
func NewSearchTool(workspace string) *SearchTool {
	return &SearchTool{
		BaseTool: BaseTool{
			ToolName:        "search",
			ToolDescription: "Search for text in workspace files and return matching lines.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {
						"type": "string",
						"description": "Text pattern to search for."
					},
					"path": {
						"type": "string",
						"description": "Relative directory path to search. Defaults to '.'"
					},
					"max_results": {
						"type": "number",
						"description": "Maximum matches to return. Defaults to 100."
					},
					"case_sensitive": {
						"type": "boolean",
						"description": "Use case-sensitive matching when true."
					}
				},
				"required": ["pattern"]
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
			RequiredFields:  []string{"pattern"},
		},
		workspace: workspace,
	}
}

func (t *SearchTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	pattern := getStringArg(input, "pattern", "")
	if pattern == "" {
		return tools.ToolResult{Error: true, Content: "pattern is required"}, nil
	}
	path := getStringArg(input, "path", ".")
	maxResults := getIntArg(input, "max_results", 100)
	if maxResults <= 0 {
		maxResults = 100
	}
	if maxResults > 1000 {
		maxResults = 1000
	}
	caseSensitive := getBoolArg(input, "case_sensitive", false)

	targetPath, realPath, err := resolvePathReal(t.workspace, path, false)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("path error: %s", err)}, nil
	}

	needle := pattern
	if !caseSensitive {
		needle = strings.ToLower(pattern)
	}

	matches := make([]map[string]any, 0, maxResults)
	truncated := false

	err = filepath.WalkDir(targetPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// A symlink inside the walk is not followed: it could point out of
		// the workspace (its in-workspace target is searched on its own).
		// The walk root was checked by resolvePathReal; open what it checked.
		name := p
		if p == targetPath {
			name = realPath
		}
		file, err := openRegularFile(name)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			haystack := line
			if !caseSensitive {
				haystack = strings.ToLower(line)
			}
			if strings.Contains(haystack, needle) {
				rel, _ := filepath.Rel(t.workspace, p)
				matches = append(matches, map[string]any{
					"path":        rel,
					"line_number": lineNumber,
					"line":        line,
				})
				if len(matches) >= maxResults {
					truncated = true
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	result := map[string]any{
		"pattern":        pattern,
		"case_sensitive": caseSensitive,
		"matches":        matches,
		"count":          len(matches),
		"truncated":      truncated,
	}

	return tools.ToolResult{
		Content:  formatResult(result),
		Metadata: result,
	}, nil
}
