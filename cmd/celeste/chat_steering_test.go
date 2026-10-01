package main

import (
	"testing"

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
