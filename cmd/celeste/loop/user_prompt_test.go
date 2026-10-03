package loop

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// upsRunner loads a hooks runner whose global hooks.json holds one
// UserPromptSubmit hook running hooktest's args; no args: no hooks file.
func upsRunner(t *testing.T, args ...string) *hooks.Runner {
	t.Helper()
	home := setupHome(t)
	if len(args) > 0 {
		write(t, filepath.Join(home, ".celeste", "hooks.json"), hooksJSON(t, hookDef("UserPromptSubmit", "", hooktest.Command(t, args...))))
	}
	r, err := hooks.Load(hooks.Options{Workspace: t.TempDir(), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// UserPromptSubmit is the one implementation the chat, MCP chat and agent
// goals share (2.0 F2e): deny blocks with the reason, context rides in
// metadata, no hooks allows the message unchanged.
func TestUserPromptSubmitVerdicts(t *testing.T) {
	msg := Message{Role: "user", Content: "hi"}
	ctx := context.Background()

	out, v, err := UserPromptSubmit(ctx, nil, msg)
	if err != nil || v.Blocked || out.Metadata != nil {
		t.Fatalf("nil runner: %+v %+v %v", out, v, err)
	}
	_, v, err = UserPromptSubmit(ctx, upsRunner(t, "deny", ""), msg)
	if err != nil || !v.Blocked || v.Reason != "no reason given" {
		t.Fatalf("deny without a reason: %+v %v", v, err)
	}
	_, v, _ = UserPromptSubmit(ctx, upsRunner(t, "deny", "nope"), msg)
	if !v.Blocked || v.Reason != "nope" {
		t.Fatalf("deny: %+v", v)
	}
	out, v, _ = UserPromptSubmit(ctx, upsRunner(t, "context", "CTX"), msg)
	if v.Blocked || out.Content != "hi" || out.Metadata[tui.MetaHookContext] != "CTX" {
		t.Fatalf("context: %+v %+v", out, v)
	}
	if msg.Metadata != nil {
		t.Fatal("the caller's message was modified")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := UserPromptSubmit(cancelled, upsRunner(t, "allow"), msg); err == nil {
		t.Fatal("an interrupted check is an error, not a verdict")
	}
}

// HookPromptCheck is nil without UserPromptSubmit hooks, so such a run takes
// the loop's unchecked path.
func TestHookPromptCheckIsNilWithoutHooks(t *testing.T) {
	if HookPromptCheck(nil) != nil || HookPromptCheck(upsRunner(t)) != nil {
		t.Fatal("a runner without UserPromptSubmit hooks gave a PromptCheck")
	}
	if HookPromptCheck(upsRunner(t, "allow")) == nil {
		t.Fatal("a runner with a UserPromptSubmit hook gave no PromptCheck")
	}
	if got := HookContextBlock("C"); got != "\n\n<hook-context>\nC\n</hook-context>" {
		t.Fatalf("HookContextBlock = %q", got)
	}
}
