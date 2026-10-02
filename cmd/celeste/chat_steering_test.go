package main

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// The chat's turns share one steering session (a rule's repeat policy
// spans the chat), rebuilt after a profile switch; stream_rules off gives
// none (2.0 W3).
func TestChatTurnsShareOneSteeringSession(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	a := deps.adapter
	if a.rules.Len() == 0 {
		t.Fatal("the chat adapter has no stream rules from its Env")
	}
	first := a.newTurnLoop(tui.TurnRequest{}, &chatTurn{})
	second := a.newTurnLoop(tui.TurnRequest{}, &chatTurn{})
	if first.Steering == nil || first.Steering != second.Steering {
		t.Fatal("turns must share the chat's steering session")
	}
	switched := *a.baseConfig
	switched.StreamRules = "off"
	a.baseConfig = &switched
	if l := a.newTurnLoop(tui.TurnRequest{}, &chatTurn{}); l.Steering != nil {
		t.Error("stream_rules off after a profile switch must give no steering")
	}
}

// A dropped reply's usage still reaches the chat's cost tracker (the
// provider billed it), without counting a turn; it shows nothing in the
// chat but the retry line.
func TestChatRecordsDroppedReplyUsage(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	a := deps.adapter
	before := a.costTracker.GetSummary()
	first := false
	msgs := a.translate(&chatTurn{model: "gpt-4.1"}, loop.Event{Kind: loop.EventRuleInterrupt, Usage: &llm.TokenUsage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}}, &first)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %v", msgs)
	}
	after := a.costTracker.GetSummary()
	if after.TotalInput != before.TotalInput+100 || after.TotalOutput != before.TotalOutput+10 {
		t.Errorf("tokens %d/%d -> %d/%d, want +100/+10", before.TotalInput, before.TotalOutput, after.TotalInput, after.TotalOutput)
	}
	// A dropped reply is not a turn (re-review item 4).
	if after.Turns != before.Turns {
		t.Errorf("cost tracker turns = %d, want %d", after.Turns, before.Turns)
	}
}

func TestLastUserTextSkipsHiddenMessages(t *testing.T) {
	h := []tui.ChatMessage{
		{Role: "user", Content: "fix the build"},
		{Role: "assistant", Content: "on it"},
		{Role: "user", Content: "<system-reminder>x</system-reminder>", Metadata: map[string]any{"hidden": true}},
	}
	if got := lastUserText(h); got != "fix the build" {
		t.Errorf("lastUserText = %q", got)
	}
}
