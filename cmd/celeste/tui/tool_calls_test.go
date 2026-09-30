package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeToolLLMClient is an LLMClient whose turns the test drives: RunTurn
// records the request and returns a fakeTurn (turn_test.go); feed delivers
// the turn's events.
type fakeToolLLMClient struct {
	skills []SkillDefinition
	turns  []*fakeTurn
}

func (f *fakeToolLLMClient) RunTurn(req TurnRequest) (TurnHandle, tea.Cmd) {
	t := &fakeTurn{req: req}
	f.turns = append(f.turns, t)
	return t, nil
}

func (f *fakeToolLLMClient) GetSkills() []SkillDefinition { return f.skills }

// Review Focus 5: the request says whether tools are offered, fixed for the
// whole turn.
func TestStartTurnOffersToolsOnlyWhenEnabled(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "go"})
	require.Len(t, client.turns, 1)
	assert.True(t, client.turns[0].req.Tools)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	m.nsfwMode = true
	m, _ = step(t, m, SendMessageMsg{Content: "again"})
	require.Len(t, client.turns, 2)
	assert.False(t, client.turns[1].req.Tools, "NSFW mode sends no tools")
}
