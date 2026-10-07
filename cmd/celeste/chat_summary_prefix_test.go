package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// CodeRabbit on #408: with no turn yet (a /compact on a resumed session)
// the summary must pick its kept tail with the estimated prefix too, not
// keep KeepFor(window) as if the prompt and the tool schemas cost nothing:
// near the limit the compacted request would still overflow the window.
func TestChatSummaryTailCountsThePrefixBeforeAnyTurn(t *testing.T) {
	const window = 32_768
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "## Goal\nchat"})
	_, deps, _ := chatAppWithContextLimit(t, srv, window)
	a := deps.adapter
	if a.overhead.Load() != 0 {
		t.Fatalf("precondition: no turn has measured the prefix, got %d", a.overhead.Load())
	}
	prefix := ctxmgr.EstimateTokens(a.client.SystemPrompt()) + compact.DefinitionTokens(a.client.GetSkills())
	// ~24k tokens of conversation: more than either tail keeps.
	history := userTurn("let us talk")
	for i := 0; i < 40; i++ {
		history = append(history,
			tui.ChatMessage{Role: "assistant", Content: fmt.Sprintf("reply %d ", i) + strings.Repeat("r", 1_200)},
			tui.ChatMessage{Role: "user", Content: fmt.Sprintf("question %d ", i) + strings.Repeat("q", 1_200)})
	}
	wantCut := compact.CutIndex(history, compact.KeepWithin(window, prefix))
	if wantCut == compact.CutIndex(history, compact.KeepFor(window)) {
		t.Fatalf("precondition: prefix %d tokens does not change the tail at %d (KeepWithin %d, KeepFor %d)",
			prefix, window, compact.KeepWithin(window, prefix), compact.KeepFor(window))
	}
	out, err := a.SummarizeContext(context.Background(), history, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Cut != wantCut {
		t.Fatalf("Cut = %d, want %d: the tail must fit KeepWithin(%d, %d prefix) = %d tokens, not KeepFor = %d",
			out.Cut, wantCut, window, prefix, compact.KeepWithin(window, prefix), compact.KeepFor(window))
	}
	if want := out.TokensAfter + prefix; out.ContextTokens != want {
		t.Fatalf("ContextTokens = %d, want %d", out.ContextTokens, want)
	}
}
