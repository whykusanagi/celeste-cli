package builtin

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// GitStatusTool runs git status and git diff --stat in the workspace.
type GitStatusTool struct {
	BaseTool
	workspace string
}

// NewGitStatusTool creates a GitStatusTool bound to the given workspace.
func NewGitStatusTool(workspace string) *GitStatusTool {
	return &GitStatusTool{
		BaseTool: BaseTool{
			ToolName:        "git_status",
			ToolDescription: "Show the current git status and diff stat for the workspace.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
		},
		workspace: workspace,
	}
}

func (t *GitStatusTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	runGit := func(args ...string) string {
		cmd := exec.CommandContext(timeout, "git", args...)
		cmd.Dir = t.workspace
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	branch := runGit("rev-parse", "--abbrev-ref", "HEAD")
	status := runGit("status", "--short")
	diffStat := runGit("diff", "--stat")

	result := map[string]any{
		"branch":    branch,
		"status":    status,
		"diff_stat": diffStat,
	}

	data, _ := json.Marshal(result)
	return tools.ToolResult{
		Content:  string(data),
		Metadata: result,
	}, nil
}
