package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
)

func trustChecker() *permissions.Checker {
	return permissions.NewChecker(permissions.PermissionConfig{Mode: permissions.ModeTrust})
}

func advisor(ask bool, calls *int) AskAdvisor {
	return func(_ context.Context, c AdvisedCall) (bool, string) {
		*calls++
		return ask, "jev_gate: destructive p=0.91"
	}
}

// An advisor's Ask on an allowed call is denied headless, with its reason
// (2.0 W3, jev_gate).
func TestAdvisorAskIsDeniedHeadless(t *testing.T) {
	executed, calls := false, 0
	r := NewRegistry()
	r.Register(execTool("bash", &executed))
	r.SetPermissionChecker(trustChecker())
	ctx := WithoutPrompt(WithAskAdvisor(context.Background(), advisor(true, &calls)))
	res, err := r.Execute(ctx, "bash", map[string]any{"command": "rm -rf build"})
	require.NoError(t, err)
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "jev_gate: destructive p=0.91")
	assert.False(t, executed)
	assert.Equal(t, 1, calls)
}

// With a prompt, the person sees the advice and can allow the call.
func TestAdvisorAskGoesToThePrompt(t *testing.T) {
	executed, calls := false, 0
	r := NewRegistry()
	r.Register(execTool("bash", &executed))
	r.SetPermissionChecker(trustChecker())
	var seen PermissionRequest
	ctx := WithPrompt(WithAskAdvisor(context.Background(), advisor(true, &calls)), func(req PermissionRequest) PermissionResponse {
		seen = req
		return PermissionResponse{Decision: "allow_once"}
	})
	res, _ := r.Execute(ctx, "bash", map[string]any{"command": "rm -rf build"})
	assert.False(t, res.Error)
	assert.True(t, executed)
	assert.Contains(t, seen.InputSummary, "[jev_gate: destructive p=0.91]")
	assert.True(t, seen.Forced, "an advisor's ask over a policy Allow is marked Forced")
}

// A policy's own Ask is not Forced: a prompt may answer it from a
// remembered "always".
func TestPolicyAskIsNotForced(t *testing.T) {
	executed := false
	r := NewRegistry()
	r.Register(execTool("write_file", &executed))
	r.SetPermissionChecker(newAskChecker())
	seen := PermissionRequest{Forced: true}
	ctx := WithPrompt(context.Background(), func(req PermissionRequest) PermissionResponse {
		seen = req
		return PermissionResponse{Decision: "allow_once"}
	})
	res, _ := r.Execute(ctx, "write_file", map[string]any{"path": "x"})
	assert.False(t, res.Error)
	assert.False(t, seen.Forced)
}

// An advisor can never allow or deny: it is not consulted on a call the
// policy denies or already asks about, and its "no" changes nothing.
func TestAdvisorOnlyAddsAnAsk(t *testing.T) {
	executed, calls := false, 0
	r := NewRegistry()
	r.Register(execTool("write_file", &executed))
	r.SetPermissionChecker(newAskChecker())
	ctx := WithoutPrompt(WithAskAdvisor(context.Background(), advisor(false, &calls)))
	res, _ := r.Execute(ctx, "write_file", map[string]any{"path": "x"})
	assert.True(t, res.Error, "the policy's Ask, headless, stays a denial")
	assert.Equal(t, 0, calls, "an advisor is not asked about a call the policy already asks about")

	r.SetPermissionChecker(trustChecker())
	res, _ = r.Execute(ctx, "write_file", map[string]any{"path": "x"})
	assert.False(t, res.Error)
	assert.True(t, executed)
	assert.Equal(t, 1, calls)
}
