package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

const maxCommandOutput = 64_000

// BashTool executes shell commands in the workspace directory.
type BashTool struct {
	BaseTool
	workspace string
}

// NewBashTool creates a BashTool bound to the given workspace directory.
func NewBashTool(workspace string) *BashTool {
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
		},
		workspace: workspace,
	}
}

func (t *BashTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	command := getStringArg(input, "command", "")
	if strings.TrimSpace(command) == "" {
		return tools.ToolResult{Error: true, Content: "command is required"}, nil
	}

	if fields := strings.Fields(command); len(fields) > 0 && (fields[0] == "sudo" || fields[0] == "su") {
		return tools.ToolResult{Error: true, Content: "sudo/su is not permitted; run commands as the current user only"}, nil
	}

	timeoutSeconds := getIntArg(input, "timeout_seconds", 20)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 20
	}
	if timeoutSeconds > 300 {
		timeoutSeconds = 300
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.Dir = t.workspace
	output, err := cmd.CombinedOutput()

	outputStr := string(output)
	truncated := false
	if len(outputStr) > maxCommandOutput {
		outputStr = outputStr[:maxCommandOutput]
		truncated = true
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	timedOut := cmdCtx.Err() == context.DeadlineExceeded

	result := map[string]any{
		"command":   command,
		"workspace": t.workspace,
		"exit_code": exitCode,
		"output":    outputStr,
		"truncated": truncated,
		"timed_out": timedOut,
	}
	if err != nil {
		result["error"] = err.Error()
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
