package tools

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The execution timeout starts once the tool is approved: a slow answer on
// the permission prompt must not hand the tool an expired context (#172).
func TestExecTimeoutStartsAfterApproval(t *testing.T) {
	var ctxErrAtStart error
	var hasDeadline bool
	r := NewRegistry()
	r.Register(&mockTool{
		name: "write_file",
		executeFunc: func(ctx context.Context, _ map[string]any, _ chan<- ProgressEvent) (ToolResult, error) {
			ctxErrAtStart = ctx.Err()
			_, hasDeadline = ctx.Deadline()
			return ToolResult{Content: "executed"}, nil
		},
	})
	r.SetPermissionChecker(newAskChecker())
	r.SetPromptFunc(func(PermissionRequest) PermissionResponse {
		time.Sleep(80 * time.Millisecond) // the user takes longer than the timeout
		return PermissionResponse{Decision: "allow_once"}
	})

	ctx := WithExecTimeout(context.Background(), 30*time.Millisecond)
	result, err := r.Execute(ctx, "write_file", map[string]any{"path": "/tmp/x"})
	require.NoError(t, err)
	assert.Equal(t, "executed", result.Content)
	assert.NoError(t, ctxErrAtStart, "tool started with an expired context")
	assert.True(t, hasDeadline, "the execution timeout was not applied")
}

// The timeout still bounds the tool itself.
func TestExecTimeoutBoundsExecution(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{
		name: "slow",
		executeFunc: func(ctx context.Context, _ map[string]any, _ chan<- ProgressEvent) (ToolResult, error) {
			select {
			case <-ctx.Done():
				return ToolResult{Content: "timed out", Error: true}, nil
			case <-time.After(2 * time.Second):
				return ToolResult{Content: "finished"}, nil
			}
		},
	})

	start := time.Now()
	result, err := r.Execute(WithExecTimeout(context.Background(), 20*time.Millisecond), "slow", nil)
	require.NoError(t, err)
	assert.Equal(t, "timed out", result.Content)
	assert.Less(t, time.Since(start), time.Second)
}
