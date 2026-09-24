package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// HookResult holds the outcome of running a hook.
type HookResult struct {
	Decision string // "approve" or "block"
	Output   string
	ExitCode int
}

// Executor runs pre/post tool-use hooks.
type Executor struct {
	hooks     []Hook
	workspace string
}

// NewExecutor creates an Executor for the given hooks and workspace directory.
func NewExecutor(hooks []Hook, workspace string) *Executor {
	return &Executor{
		hooks:     hooks,
		workspace: workspace,
	}
}

// RunPreToolUse runs all matching PreToolUse hooks.
// Returns the first blocking result, or an approve result if all pass.
func (e *Executor) RunPreToolUse(toolName string, input map[string]any) (*HookResult, error) {
	return e.runHooks("PreToolUse", toolName, input)
}

// RunPostToolUse runs all matching PostToolUse hooks.
func (e *Executor) RunPostToolUse(toolName string, input map[string]any) (*HookResult, error) {
	return e.runHooks("PostToolUse", toolName, input)
}

func (e *Executor) runHooks(event, toolName string, input map[string]any) (*HookResult, error) {
	last := &HookResult{Decision: "approve"}
	for _, h := range e.hooks {
		if h.Event != event {
			continue
		}
		if h.Tool != "*" && h.Tool != toolName {
			continue
		}

		result, err := e.executeHook(h, event, toolName, input)
		if err != nil {
			return nil, fmt.Errorf("hook execution failed: %w", err)
		}
		if result.Decision == "block" {
			return result, nil
		}
		last = result
	}
	return last, nil
}

func (e *Executor) executeHook(h Hook, event, toolName string, input map[string]any) (*HookResult, error) {
	cmd := expandTemplateVars(h.Command, e.workspace, toolName, input)

	timeout := h.Timeout
	if timeout <= 0 {
		timeout = defaultHookTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	proc := exec.CommandContext(ctx, "sh", "-c", cmd)
	proc.Dir = e.workspace

	// The payload also reaches the hook as data, never as shell text: JSON on
	// stdin and CELESTE_* environment variables. Hooks that read these need no
	// template substitution at all.
	payload, err := json.Marshal(map[string]any{
		"event":      event,
		"tool_name":  toolName,
		"tool_input": input,
		"workspace":  e.workspace,
	})
	if err != nil {
		return nil, fmt.Errorf("encode hook payload: %w", err)
	}
	proc.Stdin = bytes.NewReader(payload)
	inputJSON, _ := json.Marshal(input)
	proc.Env = append(os.Environ(),
		"CELESTE_HOOK_EVENT="+event,
		"CELESTE_TOOL_NAME="+toolName,
		"CELESTE_TOOL_INPUT="+string(inputJSON),
		"CELESTE_TOOL_PATH="+stringArg(input, "path"),
		"CELESTE_TOOL_COMMAND="+stringArg(input, "command"),
		"CELESTE_WORKSPACE="+e.workspace,
	)

	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr

	err = proc.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		output = strings.TrimSpace(stderr.String())
	}

	decision := "approve"
	if exitCode != 0 {
		decision = "block"
	}

	return &HookResult{
		Decision: decision,
		Output:   output,
		ExitCode: exitCode,
	}, nil
}

// expandTemplateVars replaces {{workspace}}, {{tool}}, {{path}} and {{command}}
// in s with shell-quoted values. {{path}} and {{command}} come from the model's
// tool arguments, and read-only tools run without an approval prompt, so an
// unquoted substitution let a crafted path such as `x; curl … | sh` execute
// through any hook that used it (#168). A placeholder the hook author already
// wrapped in quotes ("{{path}}" or '{{path}}') is replaced whole, so the value
// is never quoted twice.
func expandTemplateVars(s, workspace, toolName string, input map[string]any) string {
	values := []struct{ name, value string }{
		{"workspace", workspace},
		{"tool", toolName},
		{"path", stringArg(input, "path")},
		{"command", stringArg(input, "command")},
	}
	// One strings.Replacer pass: substituted text is never rescanned, so a
	// value containing "{{command}}" can't pull a second substitution into
	// its quotes. Quoted forms come first so they win over the bare form.
	var pairs []string
	for _, v := range values {
		placeholder := "{{" + v.name + "}}"
		quoted := shellQuote(v.value)
		pairs = append(pairs,
			`"`+placeholder+`"`, quoted,
			`'`+placeholder+`'`, quoted,
			placeholder, quoted,
		)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// stringArg returns input[key] when it is a string, or "".
func stringArg(input map[string]any, key string) string {
	v, _ := input[key].(string)
	return v
}

// shellQuote returns s as a single POSIX shell word: wrapped in single quotes,
// with each embedded single quote written as '\”.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
