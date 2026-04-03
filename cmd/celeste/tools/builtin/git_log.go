package builtin

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// GitLogTool runs git log with configurable options.
type GitLogTool struct {
	BaseTool
	workspace string
}

// NewGitLogTool creates a GitLogTool bound to the given workspace.
func NewGitLogTool(workspace string) *GitLogTool {
	return &GitLogTool{
		BaseTool: BaseTool{
			ToolName:        "git_log",
			ToolDescription: "Show git commit history with configurable filters.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"count": {
						"type": "number",
						"description": "Number of commits to show (default 10)."
					},
					"author": {
						"type": "string",
						"description": "Filter by author name or email."
					},
					"since": {
						"type": "string",
						"description": "Show commits after date, e.g. '2024-01-01' or '1 week ago'."
					},
					"path": {
						"type": "string",
						"description": "Filter commits affecting this file or directory path."
					}
				},
				"required": []
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
		},
		workspace: workspace,
	}
}

func (t *GitLogTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	count := getIntArg(input, "count", 10)
	if count <= 0 {
		count = 10
	}
	if count > 100 {
		count = 100
	}

	// Use a delimiter format for reliable parsing.
	args := []string{
		"log",
		"--format=%H\x1f%s\x1f%an\x1f%ai",
		"-n", strconv.Itoa(count),
	}

	if author := getStringArg(input, "author", ""); author != "" {
		args = append(args, "--author="+author)
	}
	if since := getStringArg(input, "since", ""); since != "" {
		args = append(args, "--since="+since)
	}

	// path must come after "--"
	if p := getStringArg(input, "path", ""); p != "" {
		args = append(args, "--", p)
	}

	cmd := exec.CommandContext(timeout, "git", args...)
	cmd.Dir = t.workspace
	out, err := cmd.Output()
	if err != nil {
		errResult := map[string]any{"error": "git log failed: " + err.Error()}
		data, _ := json.Marshal(errResult)
		return tools.ToolResult{Content: string(data), Error: true}, nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	commits := make([]map[string]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 4)
		if len(parts) < 4 {
			continue
		}
		commits = append(commits, map[string]string{
			"hash":    parts[0],
			"message": parts[1],
			"author":  parts[2],
			"date":    parts[3],
		})
	}

	result := map[string]any{"commits": commits}
	data, _ := json.Marshal(result)
	return tools.ToolResult{
		Content:  string(data),
		Metadata: result,
	}, nil
}
