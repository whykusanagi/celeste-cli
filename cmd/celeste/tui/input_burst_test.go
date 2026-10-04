package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// burst splits s the way Bubble Tea's reader parses one write from a
// terminal without bracketed paste: each run of printable characters is
// one KeyRunes message, each space a KeySpace.
func burst(s string) []tea.KeyMsg {
	var out []tea.KeyMsg
	for i, word := range strings.Split(s, " ") {
		if i > 0 {
			out = append(out, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
		}
		if word != "" {
			out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(word)})
		}
	}
	return out
}

// Words that are key names ("left", "tab", "ctrl+c") in a burst.
const keyNameText = "A left B right C up D down E tab F home G end H esc I pgup J delete K ctrl+c L ctrl+u M alt+b N"

// #320: printable text that arrives as runes is inserted, never taken
// for a named key.
func TestInputBurstKeepsKeyNameWords(t *testing.T) {
	m := NewInputModel().Focus()
	for _, k := range burst(keyNameText) {
		m, _ = m.Update(k)
	}
	assert.Equal(t, keyNameText, m.Value())
}

// The same burst through the chat: nothing is interrupted, cancelled or
// scrolled, and every word reaches the input.
func TestAppBurstKeepsKeyNameWords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var m tea.Model = NewApp(nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	for _, k := range burst("its left half and its right half " + keyNameText) {
		m, _ = m.Update(k)
	}
	app := m.(AppModel)
	assert.Equal(t, "its left half and its right half "+keyNameText, app.input.Value())
	assert.False(t, app.interruptPending, "the word ctrl+c was taken for Ctrl+C")
}

// A list drops a pasted word: "end" is not the End key.
func TestSelectorBurstIsDropped(t *testing.T) {
	items := []SelectorItem{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	m := NewApp(nil)
	m.selector = NewSelectorModel("pick", items)
	m.selectorActive = true
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("end")})
	require.True(t, m.selectorActive)
	assert.Equal(t, 0, m.selector.selected, "the word end jumped to the last item")
}

func TestKeyName(t *testing.T) {
	assert.Equal(t, "left", keyName(tea.KeyMsg{Type: tea.KeyLeft}))
	assert.Equal(t, "j", keyName(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}))
	assert.Equal(t, "", keyName(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("left")}))
}

// A pasted word in a command-only view runs none of its letters: "dog"
// in the session picker must not delete a session with its d.
func TestSessionPanelBurstDeletesNothing(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	panel := NewSessionPanelModel("")
	require.NotEmpty(t, panel.entries)
	m.sessionPanel = &panel
	m.viewMode = "sessions"
	for _, word := range []string{"dog", "delete", "down"} {
		m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(word)})
	}
	_, err := mgr.mgr.Load(other.ID)
	require.NoError(t, err, "a pasted word deleted a session")
	assert.Equal(t, "sessions", m.viewMode)
}

// The skills filter takes a burst as typed text.
func TestSkillsFilterTakesBurst(t *testing.T) {
	m := NewApp(nil)
	b := NewSkillsBrowserModel(nil)
	m.skillsBrowser = &b
	m.viewMode = "skills"
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("left")})
	assert.Equal(t, "left", m.skillsBrowser.query)
}

// Graph search takes a burst letter by letter, as typed.
func TestGraphSearchTakesBurst(t *testing.T) {
	m := NewApp(nil)
	m.graphModel = &GraphModel{searching: true}
	m.viewMode = "graph"
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("left")})
	assert.Equal(t, "left", m.graphModel.searchQuery)
}
