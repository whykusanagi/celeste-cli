package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAskPrompt_SingleSelectReturnsLabel(t *testing.T) {
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(AskRequestMsg{
		Question: "color?",
		Options:  []AskOption{{Label: "red"}, {Label: "blue"}},
		Response: ch,
	})
	require.True(t, m.Active())

	// move to "blue", press enter
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	resp := <-ch
	assert.False(t, resp.Cancelled)
	assert.Equal(t, []string{"blue"}, resp.Selected)
	assert.False(t, m.Active())
}

func TestAskPrompt_EscCancels(t *testing.T) {
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(AskRequestMsg{Question: "q", Options: []AskOption{{Label: "x"}}, Response: ch})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // side effect: sends on ch
	resp := <-ch
	assert.True(t, resp.Cancelled)
}

func TestAskPrompt_MultiSelectSpaceThenEnter(t *testing.T) {
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(AskRequestMsg{
		Question: "toppings", MultiSelect: true,
		Options:  []AskOption{{Label: "cheese"}, {Label: "ham"}},
		Response: ch,
	})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggle cheese
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // -> ham
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggle ham
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm; side effect: sends on ch
	resp := <-ch
	assert.ElementsMatch(t, []string{"cheese", "ham"}, resp.Selected)
}
