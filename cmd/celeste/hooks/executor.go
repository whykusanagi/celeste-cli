package hooks

import (
	"bytes"
	"context"
	"fmt"
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

		result, err := e.executeHook(h, toolName, input)
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

func (e *Executor) executeHook(h Hook, toolName string, input map[string]any) (*HookResult, error) {
	cmd := expandTemplateVars(h.Command, e.workspace, toolName, input)

	timeout := h.Timeout
	if timeout <= 0 {
		timeout = defaultHookTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	proc := exec.CommandContext(ctx, "sh", "-c", cmd)
	proc.Dir = e.workspace

	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr

	err := proc.Run()

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

// expandTemplateVars replaces {{workspace}}, {{tool}}, {{path}}, {{command}} in s.
func expandTemplateVars(s, workspace, toolName string, input map[string]any) string {
	s = strings.ReplaceAll(s, "{{workspace}}", workspace)
	s = strings.ReplaceAll(s, "{{tool}}", toolName)

	if p, ok := input["path"].(string); ok {
		s = strings.ReplaceAll(s, "{{path}}", p)
	} else {
		s = strings.ReplaceAll(s, "{{path}}", "")
	}

	if c, ok := input["command"].(string); ok {
		s = strings.ReplaceAll(s, "{{command}}", c)
	} else {
		s = strings.ReplaceAll(s, "{{command}}", "")
	}

	return s
}
