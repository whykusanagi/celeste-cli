package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stoppedKeyClock freezes the key clock: every key arrives in the same
// instant, as a paste does on a terminal without bracketed paste. The
// returned func moves the clock on.
func stoppedKeyClock(t *testing.T) func(time.Duration) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := keyClock
	keyClock = func() time.Time { return now }
	t.Cleanup(func() { keyClock = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

// humanPace sets the key clock to move on a quarter second per reading:
// keys as a person types them.
func humanPace(t *testing.T) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := keyClock
	keyClock = func() time.Time { now = now.Add(250 * time.Millisecond); return now }
	t.Cleanup(func() { keyClock = prev })
}

var enterKey = tea.KeyMsg{Type: tea.KeyEnter}

// #326: a pasted digit and newline (one burst) never answers the ask
// modal; the cursor goes back to the safe first option with the hint.
func TestAskPromptPastedDigitEnterDoesNotAnswer(t *testing.T) {
	for name, keys := range map[string][]tea.KeyMsg{
		"digit":           {runeKey('2'), enterKey},
		"text then digit": {runeKey('x'), enterKey, runeKey('2'), enterKey},
		"j then newline":  {runeKey('j'), enterKey},
	} {
		stoppedKeyClock(t)
		ch := make(chan AskResponseMsg, 1)
		m := NewAskPromptModel()
		m, _ = m.Update(planApprovalRequest(ch))
		for _, k := range keys {
			m, _ = m.Update(k)
		}
		_, answered := pending(ch)
		require.False(t, answered, "%s: a pasted burst answered the modal", name)
		assert.Contains(t, m.View(), "› Keep planning", name)
		assert.Contains(t, m.View(), "Choose an option", name)
	}
}

// A multi-select paste of "1 " and a newline does not submit either.
func TestAskPromptPastedToggleEnterDoesNotAnswer(t *testing.T) {
	stoppedKeyClock(t)
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(AskRequestMsg{Question: "which?", MultiSelect: true,
		Options: []AskOption{{Label: "a"}, {Label: "b"}}, Response: ch})
	m, _ = m.Update(runeKey('2'))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	_, _ = m.Update(enterKey)
	_, answered := pending(ch)
	assert.False(t, answered)
}

// Keyboard use stays natural: a number, a pause, Enter answers.
func TestAskPromptDigitThenEnterAnswers(t *testing.T) {
	advance := stoppedKeyClock(t)
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(planApprovalRequest(ch))
	m, _ = m.Update(runeKey('2'))
	advance(150 * time.Millisecond)
	_, _ = m.Update(enterKey)
	r, ok := pending(ch)
	require.True(t, ok)
	assert.Equal(t, []string{"Approve and start"}, r.Selected)
}

// After a burst Enter, the user can still choose: the hint clears on
// Enter and the next deliberate pick answers.
func TestAskPromptPickAfterBurstAnswers(t *testing.T) {
	advance := stoppedKeyClock(t)
	ch := make(chan AskResponseMsg, 1)
	m := NewAskPromptModel()
	m, _ = m.Update(planApprovalRequest(ch))
	m, _ = m.Update(runeKey('2'))
	m, _ = m.Update(enterKey)
	advance(time.Second)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	advance(time.Second)
	_, _ = m.Update(enterKey)
	r, ok := pending(ch)
	require.True(t, ok)
	assert.Equal(t, []string{"Approve and start"}, r.Selected)
}

// The permission modal has the same hole: a pasted "a" and newline
// allowed the tool.
func TestPermissionPromptPastedAllowEnterDoesNotAllow(t *testing.T) {
	for _, key := range []rune{'a', 'A'} {
		stoppedKeyClock(t)
		ch := make(chan PermissionResponse, 1)
		m := openPermission(ch, 80)
		m, _ = m.Update(runeKey(key))
		m, _ = m.Update(enterKey)
		_, answered := pendingPermission(ch)
		require.False(t, answered, "pasted %q + newline answered", key)
		require.True(t, m.Active())
	}
}

func TestPermissionPromptAllowThenEnterAllows(t *testing.T) {
	advance := stoppedKeyClock(t)
	ch := make(chan PermissionResponse, 1)
	m := openPermission(ch, 80)
	m, _ = m.Update(runeKey('a'))
	advance(150 * time.Millisecond)
	_, _ = m.Update(enterKey)
	r, ok := pendingPermission(ch)
	require.True(t, ok)
	assert.Equal(t, "allow_once", r.Decision)
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
