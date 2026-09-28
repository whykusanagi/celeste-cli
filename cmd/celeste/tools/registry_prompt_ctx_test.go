package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
)

type writeTool struct{ timeoutTool }

func (writeTool) Name() string     { return "w" }
func (writeTool) IsReadOnly() bool { return false }

func askRegistry() *Registry {
	r := NewRegistry()
	r.Register(writeTool{})
	r.SetPermissionChecker(permissions.NewChecker(permissions.DefaultConfig())) // writes Ask
	return r
}

// A run brings its own Gate through the context; the registry-wide prompt
// is not consulted (2.0 F2).
func TestContextPromptAnswersAsk(t *testing.T) {
	r := askRegistry()
	r.SetPromptFunc(func(PermissionRequest) PermissionResponse {
		t.Fatal("registry prompt must not be called when the context carries one")
		return PermissionResponse{}
	})
	var asked string
	ctx := WithPrompt(context.Background(), func(req PermissionRequest) PermissionResponse {
		asked = req.ToolName
		return PermissionResponse{Decision: "allow_once"}
	})
	res, err := r.Execute(ctx, "w", map[string]any{})
	if err != nil || res.Error {
		t.Fatalf("res=%+v err=%v, want the call to run", res, err)
	}
	if asked != "w" {
		t.Fatalf("context prompt saw %q, want w", asked)
	}
}

func TestNoPromptAnywhereStillDenies(t *testing.T) {
	res, _ := askRegistry().Execute(context.Background(), "w", map[string]any{})
	if !res.Error || !strings.Contains(res.Content, "no prompt is configured") {
		t.Fatalf("got %+v, want the headless denial", res)
	}
}

// A run without a Gate denies as headless even when the registry has its
// own prompt (2.0 F2a: the TUI's modal must not answer a Gate-less run).
func TestWithoutPromptDeniesAsHeadless(t *testing.T) {
	r := askRegistry()
	r.SetPromptFunc(func(PermissionRequest) PermissionResponse {
		t.Fatal("registry prompt must not be called for a run without a Gate")
		return PermissionResponse{}
	})
	res, _ := r.Execute(WithoutPrompt(context.Background()), "w", map[string]any{})
	if !res.Error || !strings.Contains(res.Content, "no prompt is configured") {
		t.Fatalf("got %+v, want the headless denial", res)
	}
}

// The innermost of WithPrompt and WithoutPrompt wins: a Gate-less subagent
// inside a gated parent is headless, and a gated run inside a Gate-less one
// asks its own Gate.
func TestInnermostPromptContextWins(t *testing.T) {
	r := askRegistry()
	allow := func(PermissionRequest) PermissionResponse { return PermissionResponse{Decision: "allow_once"} }

	res, _ := r.Execute(WithoutPrompt(WithPrompt(context.Background(), allow)), "w", map[string]any{})
	if !res.Error || !strings.Contains(res.Content, "no prompt is configured") {
		t.Fatalf("gated parent, Gate-less child: got %+v, want the headless denial", res)
	}
	res, err := r.Execute(WithPrompt(WithoutPrompt(context.Background()), allow), "w", map[string]any{})
	if err != nil || res.Error {
		t.Fatalf("Gate-less parent, gated child: res=%+v err=%v, want the call to run", res, err)
	}
}
