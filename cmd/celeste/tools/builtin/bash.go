package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/sandbox"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

const maxCommandOutput = 64_000

// BashTool executes shell commands in the workspace directory.
type BashTool struct {
	BaseTool
	workspace string
	policy    *sandbox.Policy // nil: no OS sandbox (the denylist alone)
	// pinned is the directory workspace named when the tool was built.
	// Commands run (and the sandbox binds) by path, so a workspace swapped
	// for another directory or a symlink since then is refused.
	pinned os.FileInfo
}

// NewBashTool creates a BashTool bound to the given workspace directory.
// Its commands run under policy's OS sandbox when it is enabled and one
// is available (2.0 W4); nil runs them with the denylist alone.
func NewBashTool(workspace string, policy *sandbox.Policy) *BashTool {
	return &BashTool{
		BaseTool: BaseTool{
			ToolName:        "bash",
			ToolDescription: "Execute a shell command from workspace root and return combined output.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {
						"type": "string",
						"description": "Shell command to execute."
					},
					"timeout_seconds": {
						"type": "number",
						"description": "Execution timeout in seconds. Defaults to 20."
					}
				},
				"required": ["command"]
			}`),
			ReadOnly:        false,
			ConcurrencySafe: false,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"command"},
			ExecTimeout:     5 * time.Minute,
		},
		workspace: workspace,
		policy:    policy,
		pinned:    statOrNil(workspace),
	}
}

func statOrNil(dir string) os.FileInfo {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	return info
}

func (t *BashTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	command := getStringArg(input, "command", "")
	if strings.TrimSpace(command) == "" {
		return tools.ToolResult{Error: true, Content: "command is required"}, nil
	}

	timeoutSeconds := getIntArg(input, "timeout_seconds", 20)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 20
	}
	if timeoutSeconds > 300 {
		timeoutSeconds = 300
	}

	if t.workspace != "" {
		// No pin (the workspace could not be read when the tool was built)
		// fails closed too.
		if cur, err := os.Stat(t.workspace); err != nil || t.pinned == nil || !os.SameFile(t.pinned, cur) {
			return tools.ToolResult{Error: true, Content: "the workspace directory changed since this session started (it was moved or replaced); bash will not run there. Restart celeste in the workspace."}, nil
		}
	}

	res := RunShell(ctx, ShellOptions{Dir: t.workspace, Command: command, Timeout: time.Duration(timeoutSeconds) * time.Second, Policy: t.policy})
	if res.Blocked != "" {
		return tools.ToolResult{Error: true, Content: res.Blocked}, nil
	}

	result := map[string]any{
		"command":   command,
		"workspace": t.workspace,
		"exit_code": res.ExitCode,
		"output":    res.Output,
		"truncated": res.Truncated,
		"timed_out": res.TimedOut,
	}
	switch {
	case res.Err != nil:
		result["error"] = res.Err.Error()
	case res.TimedOut:
		result["error"] = fmt.Sprintf("timed out after %ds; the command and everything it started were killed", timeoutSeconds)
	case ctx.Err() != nil:
		result["error"] = "cancelled; the command and everything it started were killed"
	case res.ExitCode != 0:
		result["error"] = fmt.Sprintf("exit status %d", res.ExitCode)
	}
	if res.Sandbox != "" {
		result["sandbox"] = res.Sandbox
	}
	if res.Hint != "" {
		if e, ok := result["error"].(string); ok {
			result["error"] = e + "; " + res.Hint
		} else {
			result["error"] = res.Hint
		}
	}

	data, _ := json.Marshal(result)
	return tools.ToolResult{
		Content:  string(data),
		Metadata: result,
	}, nil
}

// formatResult marshals a result map to JSON for ToolResult.Content.
func formatResult(result map[string]any) string {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("%v", result)
	}
	return string(data)
}
