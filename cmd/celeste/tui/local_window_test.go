package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeWindowClient struct{ follows int }

func (f *fakeWindowClient) RunTurn(TurnRequest) (TurnHandle, tea.Cmd) { return nil, nil }
func (f *fakeWindowClient) GetSkills() []SkillDefinition              { return nil }
func (f *fakeWindowClient) FollowWindow()                             { f.follows++ }

// #310 review: a local server's window arrives from a background probe as
// LocalWindowMsg; an idle chat follows it at once, a running turn leaves it
// to the next turn (RunTurn follows the window before its first request).
func TestLocalWindowMsgFollowsWhenIdle(t *testing.T) {
	client := &fakeWindowClient{}
	m := NewApp(client)
	m, _ = step(t, m, LocalWindowMsg{})
	if client.follows != 1 {
		t.Fatalf("idle: %d follows, want 1", client.follows)
	}
	m.streaming = true
	step(t, m, LocalWindowMsg{})
	if client.follows != 1 {
		t.Fatalf("mid-turn: %d follows, want 1", client.follows)
	}
}
