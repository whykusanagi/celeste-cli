package tools

import (
	"context"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
)

// stateTool is a write tool whose "note" calls only change internal state.
type stateTool struct{ mockTool }

func (s *stateTool) InternalState(input map[string]any) bool { return input["action"] == "note" }

// #357: a call that only changes internal state is read-only to the
// permission checker: no prompt in the default mode. Its other calls, and
// strict mode, still ask; the tool itself stays non-read-only.
func TestInternalStateCallsSkipThePrompt(t *testing.T) {
	for _, tc := range []struct {
		mode   permissions.PermissionMode
		action string
		ask    bool
	}{
		{permissions.ModeDefault, "note", false},
		{permissions.ModeDefault, "erase", true},
		{permissions.ModeStrict, "note", true},
	} {
		r := NewRegistry()
		tool := &stateTool{mockTool{name: "state"}}
		r.Register(tool)
		r.SetPermissionChecker(permissions.NewChecker(permissions.PermissionConfig{Mode: tc.mode}))
		asked := false
		r.SetPromptFunc(func(PermissionRequest) PermissionResponse {
			asked = true
			return PermissionResponse{Decision: "allow_once"}
		})
		res, err := r.ExecuteWithProgress(context.Background(), "state", map[string]any{"action": tc.action}, nil)
		if err != nil || res.Error {
			t.Fatalf("%s %s: %v %+v", tc.mode, tc.action, err, res)
		}
		if asked != tc.ask {
			t.Fatalf("%s %s: asked = %v, want %v", tc.mode, tc.action, asked, tc.ask)
		}
		if tool.IsReadOnly() {
			t.Fatal("the tool itself must stay non-read-only")
		}
	}
}

// The jev_gate advisor is told an internal-state call is read-only, so it
// is not consulted about bookkeeping.
func TestInternalStateCallsReadOnlyToTheAdvisor(t *testing.T) {
	r := NewRegistry()
	r.Register(&stateTool{mockTool{name: "state"}})
	r.SetPermissionChecker(permissions.NewChecker(permissions.PermissionConfig{Mode: permissions.ModeDefault}))
	var got AdvisedCall
	ctx := WithAskAdvisor(context.Background(), func(_ context.Context, c AdvisedCall) (bool, string) {
		got = c
		return false, ""
	})
	if _, err := r.ExecuteWithProgress(ctx, "state", map[string]any{"action": "note"}, nil); err != nil {
		t.Fatal(err)
	}
	if !got.ReadOnly {
		t.Fatalf("advised call = %+v, want ReadOnly", got)
	}
}
