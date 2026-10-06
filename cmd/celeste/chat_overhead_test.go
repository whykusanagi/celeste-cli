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

// smokeOverhead is the L2 smoke run's fixed prefix: the spine persona and
// 49 tool schemas.
const smokeOverhead = 16_500

// L2: a 32k chat whose system prompt and tool schemas are ~16k. A short
// exchange must not mark the turn for a summary ("Summarizing older
// context…" then "Summary skipped" every turn).
func TestChatCompactorNoSummaryLoopAt32k(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	history := userTurn("hello " + strings.Repeat("h", 800))
	history = append(history, tui.ChatMessage{Role: "assistant", Content: "hi there " + strings.Repeat("t", 800)})
	history = append(history, userTurn("how are you "+strings.Repeat("w", 800))...)
	c := &chatCompactor{a: deps.adapter, window: 32_768, meter: compact.NewMeter(smokeOverhead)}
	if _, _, changed := c.Compact(context.Background(), history, nil, false); changed {
		t.Fatal("a three-message chat was compacted")
	}
	if c.over.Load() {
		t.Fatal("a three-message chat under a 16k prefix asked for a summary")
	}
}

// When the 32k chat does need a summary, the summary it asks for keeps a
// tail the history can afford, so it is not skipped.
func TestChatSummaryAt32kFollowsTheCompactor(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "## Goal\nchat"})
	_, deps, _ := chatAppWithContextLimit(t, srv, 32_768)
	history := userTurn("let us talk")
	for i := 0; i < 12; i++ { // ~9.6k tokens of conversation
		history = append(history,
			tui.ChatMessage{Role: "assistant", Content: fmt.Sprintf("reply %d ", i) + strings.Repeat("r", 1_600)},
			tui.ChatMessage{Role: "user", Content: fmt.Sprintf("question %d ", i) + strings.Repeat("q", 1_600)})
	}
	c := &chatCompactor{a: deps.adapter, window: 32_768, meter: compact.NewMeter(smokeOverhead)}
	c.Compact(context.Background(), history, nil, false)
	if !c.over.Load() {
		t.Fatal("~9.6k of history over a 16k prefix at 32k should ask for a summary")
	}
	out, err := deps.adapter.SummarizeContext(context.Background(), history, "")
	if err != nil {
		t.Fatalf("the summary the compactor asked for failed: %v", err)
	}
	if out.Cut == 0 {
		t.Fatal("nothing was summarized")
	}
}

// #400: the summary reports the next request's size, its fixed prefix
// (the one the compactor measured) included, so the header and the bar do
// not drop to the summary's own size after an automatic summary.
func TestChatSummaryReportsTheNextRequestSize(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "## Goal\nchat"})
	_, deps, _ := chatAppWithContextLimit(t, srv, 32_768)
	history := userTurn("let us talk")
	for i := 0; i < 12; i++ {
		history = append(history,
			tui.ChatMessage{Role: "assistant", Content: fmt.Sprintf("reply %d ", i) + strings.Repeat("r", 1_600)},
			tui.ChatMessage{Role: "user", Content: fmt.Sprintf("question %d ", i) + strings.Repeat("q", 1_600)})
	}
	c := &chatCompactor{a: deps.adapter, window: 32_768, meter: compact.NewMeter(smokeOverhead)}
	c.Compact(context.Background(), history, nil, false)
	out, err := deps.adapter.SummarizeContext(context.Background(), history, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.TokensAfter <= 0 {
		t.Fatal("no TokensAfter")
	}
	if got := out.ContextTokens - out.TokensAfter; got < smokeOverhead {
		t.Fatalf("ContextTokens %d = TokensAfter %d + %d, want the ~%d-token prefix on top", out.ContextTokens, out.TokensAfter, got, smokeOverhead)
	}
}

// With no turn yet (a /compact on a resumed session) the prefix is the
// system prompt and the tool schemas the next request offers.
func TestChatSummaryCountsThePrefixBeforeAnyTurn(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "## Goal\nread the files"})
	_, deps, _ := chatApp(t, srv)
	out, err := deps.adapter.SummarizeContext(context.Background(), bigToolHistory(), "")
	if err != nil {
		t.Fatal(err)
	}
	a := deps.adapter
	want := out.TokensAfter + ctxmgr.EstimateTokens(a.client.SystemPrompt()) + compact.DefinitionTokens(a.client.GetSkills())
	if out.ContextTokens != want {
		t.Fatalf("ContextTokens = %d, want %d (TokensAfter %d + system prompt + tool schemas)", out.ContextTokens, want, out.TokensAfter)
	}
}

// Review: the prefix the compactor last measured belongs to the endpoint,
// model, prompt and tools it measured. After a switch a summary falls back
// to the new system prompt and tool schemas, not the old prefix.
func TestChatSummaryDropsAStalePrefixAfterASwitch(t *testing.T) {
	for name, change := range map[string]func(a *TUIClientAdapter){
		"model":    func(a *TUIClientAdapter) { _ = a.ChangeModel(a.client.GetConfig().Model + "-other") },
		"endpoint": func(a *TUIClientAdapter) { _ = a.RestoreEndpoint(a.SnapshotEndpoint()) },
		"prompt":   func(a *TUIClientAdapter) { a.RefreshSystemPrompt() },
	} {
		t.Run(name, func(t *testing.T) {
			srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "## Goal\nread the files"})
			_, deps, _ := chatApp(t, srv)
			a := deps.adapter
			a.overhead.Store(50_000) // measured on the endpoint being left
			change(a)
			out, err := a.SummarizeContext(context.Background(), bigToolHistory(), "")
			if err != nil {
				t.Fatal(err)
			}
			want := out.TokensAfter + ctxmgr.EstimateTokens(a.client.SystemPrompt()) + compact.DefinitionTokens(a.client.GetSkills())
			if out.ContextTokens != want {
				t.Fatalf("ContextTokens = %d, want %d (the new prefix, not the stale 50k)", out.ContextTokens, want)
			}
		})
	}
}
