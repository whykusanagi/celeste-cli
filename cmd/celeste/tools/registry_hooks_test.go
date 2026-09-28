package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
)

type fakeHooks struct {
	pre         PreToolHookResult
	post        string
	preInputs   []map[string]any
	postResults []ToolResult
	postCtxErr  error
}

func (f *fakeHooks) PreToolUse(ctx context.Context, name string, input map[string]any) PreToolHookResult {
	f.preInputs = append(f.preInputs, input)
	return f.pre
}

func (f *fakeHooks) PostToolUse(ctx context.Context, name string, input map[string]any, result ToolResult) string {
	f.postResults = append(f.postResults, result)
	f.postCtxErr = ctx.Err()
	return f.post
}

// validatingTool rejects path "bad", like a real tool's ValidateInput.
type validatingTool struct{ mockTool }

func (v *validatingTool) ValidateInput(input map[string]any) error {
	if input["path"] == "bad" {
		return errors.New("path must not be bad")
	}
	return nil
}

func recordingTool(name string, got *map[string]any) *mockTool {
	return &mockTool{name: name, executeFunc: func(ctx context.Context, input map[string]any, _ chan<- ProgressEvent) (ToolResult, error) {
		*got = input
		return ToolResult{Content: "executed"}, nil
	}}
}

func denyRule(pattern, input string) *permissions.Checker {
	return permissions.NewChecker(permissions.PermissionConfig{
		Mode:       permissions.ModeTrust,
		AlwaysDeny: []permissions.Rule{{ToolPattern: pattern, InputPattern: input, Decision: permissions.Deny}},
	})
}

// Review fix I4: a hard denial never reaches a hook process.
func TestHardDenyNeverReachesHooks(t *testing.T) {
	var got map[string]any
	r := NewRegistry()
	r.Register(recordingTool("write_file", &got))
	r.SetPermissionChecker(denyRule("write_file", ""))
	h := &fakeHooks{pre: PreToolHookResult{Decision: "allow"}}
	r.SetHookRunner(h)
	res, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "x"})
	require.NoError(t, err)
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "Permission denied")
	assert.Empty(t, h.preInputs, "PreToolUse must not run for a hard-denied call")
}

func TestHookDenyBlocksBeforePermissionPrompt(t *testing.T) {
	var got map[string]any
	r := NewRegistry()
	r.Register(recordingTool("write_file", &got))
	r.SetPermissionChecker(newAskChecker())
	asked := 0
	r.SetPromptFunc(func(PermissionRequest) PermissionResponse { asked++; return PermissionResponse{Decision: "allow_once"} })
	r.SetHookRunner(&fakeHooks{pre: PreToolHookResult{Decision: "deny", Reason: "nope"}})
	res, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "x"})
	require.NoError(t, err)
	assert.True(t, res.Error)
	assert.Equal(t, "Blocked by pre-tool hook: nope", res.Content)
	assert.Zero(t, asked, "a denied call never prompts")
	assert.Nil(t, got)
}

func TestHookAskForcesPromptEvenWhenAllowed(t *testing.T) {
	var got map[string]any
	r := NewRegistry()
	r.Register(recordingTool("write_file", &got))
	r.SetPermissionChecker(permissions.NewChecker(permissions.PermissionConfig{Mode: permissions.ModeTrust}))
	asked := 0
	r.SetPromptFunc(func(PermissionRequest) PermissionResponse { asked++; return PermissionResponse{Decision: "allow_once"} })
	r.SetHookRunner(&fakeHooks{pre: PreToolHookResult{Decision: "ask"}})
	res, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "x"})
	require.NoError(t, err)
	assert.False(t, res.Error)
	assert.Equal(t, 1, asked)
}

func TestHookAskWithoutPromptDenies(t *testing.T) {
	var got map[string]any
	r := NewRegistry()
	r.Register(recordingTool("write_file", &got))
	r.SetHookRunner(&fakeHooks{pre: PreToolHookResult{Decision: "ask"}})
	res, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "x"})
	require.NoError(t, err)
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "Permission denied")
	assert.Nil(t, got)
}

func TestHookAllowDoesNotSkipThePrompt(t *testing.T) {
	var got map[string]any
	r := NewRegistry()
	r.Register(recordingTool("write_file", &got))
	r.SetPermissionChecker(newAskChecker())
	r.SetPromptFunc(func(PermissionRequest) PermissionResponse { return PermissionResponse{Decision: "deny"} })
	r.SetHookRunner(&fakeHooks{pre: PreToolHookResult{Decision: "allow"}})
	res, _ := r.Execute(context.Background(), "write_file", map[string]any{"path": "x"})
	assert.True(t, res.Error)
	assert.Nil(t, got)
}

func TestHookUpdatedInputIsRevalidated(t *testing.T) {
	executed := false
	r := NewRegistry()
	r.Register(&validatingTool{mockTool{name: "write_file", executeFunc: func(context.Context, map[string]any, chan<- ProgressEvent) (ToolResult, error) {
		executed = true
		return ToolResult{Content: "executed"}, nil
	}}})
	r.SetHookRunner(&fakeHooks{pre: PreToolHookResult{Decision: "allow", UpdatedInput: map[string]any{"path": "bad"}}})
	res, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "ok"})
	require.NoError(t, err)
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "path must not be bad")
	assert.False(t, executed)
}

func TestHookUpdatedInputIsPermissionChecked(t *testing.T) {
	var got map[string]any
	r := NewRegistry()
	r.Register(recordingTool("write_file", &got))
	r.SetPermissionChecker(denyRule("write_file", "*secret*"))
	h := &fakeHooks{pre: PreToolHookResult{Decision: "allow", UpdatedInput: map[string]any{"path": "secret.txt"}}}
	r.SetHookRunner(h)
	res, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "ok.txt"})
	require.NoError(t, err)
	assert.Len(t, h.preInputs, 1, "the original input passed the deny pass, so hooks ran")
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "Permission denied")
	assert.Nil(t, got)
}

func TestHookUpdatedInputReachesTool(t *testing.T) {
	var got map[string]any
	r := NewRegistry()
	r.Register(recordingTool("write_file", &got))
	r.SetHookRunner(&fakeHooks{pre: PreToolHookResult{Decision: "allow", UpdatedInput: map[string]any{"path": "safe.txt"}}})
	_, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "x.txt"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"path": "safe.txt"}, got)
}

// Review fix I6: context is prepended, so capping the tail of a large result
// can't drop it.
func TestHookContextPrependedAndPostSeesResult(t *testing.T) {
	var got map[string]any
	r := NewRegistry()
	r.Register(recordingTool("read_file", &got))
	h := &fakeHooks{pre: PreToolHookResult{Decision: "allow", AdditionalContext: "PRE"}, post: "LINT: ok"}
	r.SetHookRunner(h)
	res, err := r.Execute(context.Background(), "read_file", map[string]any{"path": "a"})
	require.NoError(t, err)
	require.Len(t, h.postResults, 1)
	assert.Equal(t, "executed", h.postResults[0].Content, "PostToolUse gets the raw result")
	assert.Equal(t, "<hook-context>\nPRE\nLINT: ok\n</hook-context>\n\nexecuted", res.Content)
	assert.True(t, strings.HasPrefix(res.Content, "<hook-context>"))
}

func TestPostHookOutlivesToolTimeout(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "slow", executeFunc: func(ctx context.Context, _ map[string]any, _ chan<- ProgressEvent) (ToolResult, error) {
		<-ctx.Done()
		return ToolResult{Content: "timed out", Error: true}, nil
	}})
	h := &fakeHooks{pre: PreToolHookResult{Decision: "allow"}}
	r.SetHookRunner(h)
	_, err := r.Execute(WithExecTimeout(context.Background(), 20*time.Millisecond), "slow", nil)
	require.NoError(t, err)
	require.Len(t, h.postResults, 1)
	assert.NoError(t, h.postCtxErr, "post hooks get the caller's context, not the expired tool timeout")
}
