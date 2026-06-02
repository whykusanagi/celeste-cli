package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
)

// newAskChecker returns a Checker that always returns Ask for any non-read-only tool.
func newAskChecker() *permissions.Checker {
	return permissions.NewChecker(permissions.PermissionConfig{
		Mode: permissions.ModeStrict, // strict = Ask for everything
	})
}

// execTool is a helper mockTool that records execution.
func execTool(name string, executed *bool) *mockTool {
	return &mockTool{
		name: name,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			*executed = true
			return ToolResult{Content: "executed"}, nil
		},
	}
}

func TestPermissionGate_DenyResponse_ToolNotExecuted(t *testing.T) {
	executed := false
	r := NewRegistry()
	r.Register(execTool("write_file", &executed))
	r.SetPermissionChecker(newAskChecker())

	// Stub prompt: always deny.
	r.SetPromptFunc(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Decision: "deny"}
	})

	result, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "/tmp/x"})
	require.NoError(t, err)
	assert.True(t, result.Error, "expected error result on deny")
	assert.Contains(t, result.Content, "Permission denied")
	assert.False(t, executed, "tool must not execute after deny")
}

func TestPermissionGate_AllowOnce_ToolExecuted(t *testing.T) {
	executed := false
	r := NewRegistry()
	r.Register(execTool("write_file", &executed))
	r.SetPermissionChecker(newAskChecker())

	// Stub prompt: allow once.
	r.SetPromptFunc(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Decision: "allow_once"}
	})

	result, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "/tmp/x"})
	require.NoError(t, err)
	assert.False(t, result.Error, "expected success on allow_once")
	assert.Equal(t, "executed", result.Content)
	assert.True(t, executed, "tool must execute after allow_once")
}

func TestPermissionGate_AlwaysDeny_ToolNotExecutedAndRulePersisted(t *testing.T) {
	executed := false
	r := NewRegistry()
	r.Register(execTool("bash", &executed))
	checker := newAskChecker()
	r.SetPermissionChecker(checker)

	// Stub prompt: always deny with a pattern.
	r.SetPromptFunc(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Decision: "always_deny", Pattern: req.ToolName}
	})

	result, err := r.Execute(context.Background(), "bash", map[string]any{"command": "rm -rf /"})
	require.NoError(t, err)
	assert.True(t, result.Error, "expected error result on always_deny")
	assert.Contains(t, result.Content, "Permission denied")
	assert.False(t, executed, "tool must not execute after always_deny")

	// The rule should now be persisted: a second execution must be denied by the
	// alwaysDeny list (without calling the prompt).
	promptCalled := false
	r.SetPromptFunc(func(req PermissionRequest) PermissionResponse {
		promptCalled = true
		return PermissionResponse{Decision: "allow_once"}
	})
	result2, err2 := r.Execute(context.Background(), "bash", map[string]any{"command": "ls"})
	require.NoError(t, err2)
	assert.True(t, result2.Error, "always_deny rule must block subsequent executions")
	assert.False(t, promptCalled, "prompt must not be invoked when always_deny rule matches")
}

func TestPermissionGate_AlwaysAllow_ToolExecutedAndRulePersisted(t *testing.T) {
	callCount := 0
	r := NewRegistry()
	r.Register(&mockTool{
		name: "search",
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			callCount++
			return ToolResult{Content: "results"}, nil
		},
	})
	checker := newAskChecker()
	r.SetPermissionChecker(checker)

	promptCalls := 0
	r.SetPromptFunc(func(req PermissionRequest) PermissionResponse {
		promptCalls++
		return PermissionResponse{Decision: "always_allow", Pattern: req.ToolName}
	})

	// First call: prompt fires, returns always_allow, tool executes.
	result, err := r.Execute(context.Background(), "search", map[string]any{"query": "foo"})
	require.NoError(t, err)
	assert.False(t, result.Error)
	assert.Equal(t, "results", result.Content)
	assert.Equal(t, 1, promptCalls, "prompt must be called on first execution")
	assert.Equal(t, 1, callCount)

	// Second call: always_allow rule now matches, prompt must not fire.
	result2, err2 := r.Execute(context.Background(), "search", map[string]any{"query": "bar"})
	require.NoError(t, err2)
	assert.False(t, result2.Error)
	assert.Equal(t, 1, promptCalls, "prompt must not be called after always_allow rule is persisted")
	assert.Equal(t, 2, callCount)
}

func TestPermissionGate_NoPromptConfigured_Denied(t *testing.T) {
	executed := false
	r := NewRegistry()
	r.Register(execTool("delete_file", &executed))
	r.SetPermissionChecker(newAskChecker())
	// No prompt func set — headless mode.

	result, err := r.Execute(context.Background(), "delete_file", map[string]any{"path": "/tmp/x"})
	require.NoError(t, err)
	assert.True(t, result.Error, "expected denial when no prompt is configured")
	assert.Contains(t, result.Content, "Permission denied")
	assert.False(t, executed, "tool must not execute when prompt is unconfigured")
}

func TestPermissionGate_EmptyDecision_Denied(t *testing.T) {
	executed := false
	r := NewRegistry()
	r.Register(execTool("bash", &executed))
	r.SetPermissionChecker(newAskChecker())

	// Prompt returns zero-value response (empty Decision).
	r.SetPromptFunc(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{}
	})

	result, err := r.Execute(context.Background(), "bash", map[string]any{"command": "ls"})
	require.NoError(t, err)
	assert.True(t, result.Error, "zero-value decision must be treated as deny")
	assert.False(t, executed)
}

func TestPermissionGate_RiskLevelClassification(t *testing.T) {
	cases := []struct {
		toolName  string
		wantLevel string
	}{
		{"bash", "destructive"},
		{"delete_file", "destructive"},
		{"remove_thing", "destructive"},
		{"write_file", "write"},
		{"edit_config", "write"},
		{"update_record", "write"},
	}

	for _, tc := range cases {
		t.Run(tc.toolName, func(t *testing.T) {
			got := classifyRiskLevel(tc.toolName)
			assert.Equal(t, tc.wantLevel, got)
		})
	}
}

func TestPermissionGate_InputSummary(t *testing.T) {
	cases := []struct {
		name     string
		input    map[string]any
		contains string
	}{
		{"command key", map[string]any{"command": "git status"}, "git status"},
		{"path key", map[string]any{"path": "/tmp/foo"}, "/tmp/foo"},
		{"empty input", map[string]any{}, "(no args)"},
		{"nil input", nil, "(no args)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inputSummary(tc.input)
			assert.Contains(t, got, tc.contains)
		})
	}
}

func TestPermissionGate_RequestFields(t *testing.T) {
	var capturedReq PermissionRequest
	r := NewRegistry()
	r.Register(&mockTool{
		name: "bash",
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			return ToolResult{Content: "ok"}, nil
		},
	})
	r.SetPermissionChecker(newAskChecker())
	r.SetPromptFunc(func(req PermissionRequest) PermissionResponse {
		capturedReq = req
		return PermissionResponse{Decision: "allow_once"}
	})

	_, _ = r.Execute(context.Background(), "bash", map[string]any{"command": "echo hello"})
	assert.Equal(t, "bash", capturedReq.ToolName)
	assert.Equal(t, "destructive", capturedReq.RiskLevel)
	assert.Contains(t, capturedReq.InputSummary, "echo hello")
}
