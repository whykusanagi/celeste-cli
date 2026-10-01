package main

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// The permission bridge tells the chat which run asked and hands it that
// run's Done (2.0 F2e).
func TestPermissionPromptCarriesTheAskingRun(t *testing.T) {
	sent := make(chan tea.Msg, 1)
	fn := permissionPrompt(func(m tea.Msg) { sent <- m })
	owner := tui.RunOwner{Kind: tui.OwnerTurn, Run: 4}
	ctx, cancel := context.WithCancel(tui.WithRunOwner(context.Background(), owner))
	defer cancel()
	got := make(chan tools.PermissionResponse, 1)
	go func() { got <- fn(tools.PermissionRequest{ToolName: "write_file", Context: ctx}) }()
	msg := (<-sent).(tui.PermissionRequestMsg)
	if msg.Owner != owner || msg.Done != ctx.Done() {
		t.Fatalf("request owner %+v done %v, want %+v and the run's Done", msg.Owner, msg.Done, owner)
	}
	msg.Response <- tui.PermissionResponse{Decision: "allow_once"}
	if r := <-got; r.Decision != "allow_once" {
		t.Fatalf("decision = %q", r.Decision)
	}
}

func TestAskPromptCarriesTheAskingRun(t *testing.T) {
	sent := make(chan tea.Msg, 1)
	fn := askPrompt(func(m tea.Msg) { sent <- m })
	owner := tui.RunOwner{Kind: tui.OwnerAgent, Run: 2}
	ctx, cancel := context.WithCancel(tui.WithRunOwner(context.Background(), owner))
	go func() { _, _ = fn(ctx, tools.AskRequest{Question: "q", Options: []tools.AskOption{{Label: "a"}}}) }()
	msg := (<-sent).(tui.AskRequestMsg)
	if msg.Owner != owner || msg.Done == nil {
		t.Fatalf("ask owner %+v done %v, want %+v and the run's Done", msg.Owner, msg.Done, owner)
	}
	cancel()
	select {
	case <-msg.Done:
	case <-time.After(time.Second):
		t.Fatal("Done is not the run's")
	}
}

// Each chat turn's context names it, so its loop's asks do too.
func TestRunTurnTagsItsContext(t *testing.T) {
	_, deps, _ := chatApp(t, fakeprovider.NewOpenAI(t))
	h, _ := deps.adapter.RunTurn(tui.TurnRequest{Run: 7})
	defer h.Cancel()
	if got := tui.RunOwnerFrom(h.(*chatTurn).ctx); got != (tui.RunOwner{Kind: tui.OwnerTurn, Run: 7}) {
		t.Fatalf("turn context owner = %+v", got)
	}
}
