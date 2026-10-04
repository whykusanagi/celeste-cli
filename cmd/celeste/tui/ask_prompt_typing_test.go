package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planApprovalRequest is the submit_plan approval as the builtin tool sends
// it: the safe answer first, so it is the pre-selected default.
func planApprovalRequest(ch chan AskResponseMsg) AskRequestMsg {
	return AskRequestMsg{
		Question: "Approve this plan?\n\n1. Do the thing",
		Options: []AskOption{
			{Label: "Keep planning"},
			{Label: "Approve and start"},
		},
		Response: ch,
	}
}

// typeText sends s one keystroke at a time, the way a terminal does.
func typeText(m AskPromptModel, s string) AskPromptModel {
	for _, r := range s {
		if r == ' ' {
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			continue
		}
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func pending(ch chan AskResponseMsg) (AskResponseMsg, bool) {
	select {
	case r := <-ch:
		return r, true
	default:
		return AskResponseMsg{}, false
	}
}

// The pre-release smoke: typing "/plan off" and Enter while the approval
// modal was open approved the plan. Typed text must never answer it.
func TestAskPromptTypedSlashCommandDoesNotApprove(t *testing.T) {
	for _, text := range []string{"/plan off", "just do it", "ok go ahead"} {
		ch := make(chan AskResponseMsg, 1)
		m := NewAskPromptModel()
		m, _ = m.Update(planApprovalRequest(ch))
		m = typeText(m, text)
		assert.Contains(t, m.View(), "Choose an option", "%q: typed keys are dropped without a hint", text)
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if r, ok := pending(ch); ok {
			assert.NotEqual(t, []string{"Approve and start"}, r.Selected, "typing %q then Enter approved the plan", text)
			continue
		}
		require.True(t, m.Active(), "%q: modal closed without an answer", text)
	}
}

// The first Enter after typing only clears the hint; the next one answers
// with the cursor, which typing put back on the safe default.
func TestAskPromptEnterAfterTypingKeepsPlanning(t *testing.T) {
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(planApprovalRequest(ch))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // on Approve
	m = typeText(m, "/plan off")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, ok := pending(ch)
	require.False(t, ok, "Enter right after typing must not answer")
	assert.NotContains(t, m.View(), "Choose an option")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r, ok := pending(ch)
	require.True(t, ok)
	assert.Equal(t, []string{"Keep planning"}, r.Selected)
}

func TestAskPromptDefaultIsFirstOption(t *testing.T) {
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(planApprovalRequest(ch))
	assert.Contains(t, m.View(), "› Keep planning")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r := <-ch
	assert.Equal(t, []string{"Keep planning"}, r.Selected)
}

func TestAskPromptExplicitApproveStillWorks(t *testing.T) {
	for _, move := range []tea.KeyMsg{
		{Type: tea.KeyDown},
		{Type: tea.KeyRunes, Runes: []rune{'j'}},
		{Type: tea.KeyRunes, Runes: []rune{'2'}},
	} {
		ch := make(chan AskResponseMsg, 1)
		m := NewAskPromptModel()
		m, _ = m.Update(planApprovalRequest(ch))
		m, _ = m.Update(move)
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		r := <-ch
		assert.Equal(t, []string{"Approve and start"}, r.Selected, "move %q", move.String())
	}
}

// After typing, an arrow is an explicit choice again.
func TestAskPromptArrowAfterTypingSelects(t *testing.T) {
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(planApprovalRequest(ch))
	m = typeText(m, "hm")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r := <-ch
	assert.Equal(t, []string{"Approve and start"}, r.Selected)
}

// A paste is typing too, whatever it contains.
func TestAskPromptPasteDoesNotAnswer(t *testing.T) {
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(planApprovalRequest(ch))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j\n"), Paste: true})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, ok := pending(ch)
	assert.False(t, ok)
	assert.True(t, strings.Contains(m.View(), "› Keep planning"))
}

// The smoke itself, through the chat: the approval modal is open and the
// user types "/plan off" and Enter into it.
func TestAppTypedPlanOffDoesNotApprovePlan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ch := make(chan AskResponseMsg, 1)
	var m tea.Model = NewApp(nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = m.Update(planApprovalRequest(ch))
	for _, r := range "/plan off" {
		k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		}
		m, _ = m.Update(k)
	}
	assert.Contains(t, m.View(), "Choose an option")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if r, ok := pending(ch); ok {
		assert.NotEqual(t, []string{"Approve and start"}, r.Selected)
	}
	// Enter now answers with the default: keep planning.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r, ok := pending(ch)
	require.True(t, ok)
	assert.Equal(t, []string{"Keep planning"}, r.Selected)
}
