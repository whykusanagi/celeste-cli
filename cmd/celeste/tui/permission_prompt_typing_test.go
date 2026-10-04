package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openPermission(ch chan PermissionResponse, width int) PermissionPromptModel {
	m := NewPermissionPromptModel()
	m.SetSize(width, 0)
	m, _ = m.Update(PermissionRequestMsg{ToolName: "write_file", InputSummary: "notes.txt", RiskLevel: "write", Response: ch})
	return m
}

func typePermission(m PermissionPromptModel, s string) PermissionPromptModel {
	for _, r := range s {
		k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		}
		m, _ = m.Update(k)
	}
	return m
}

func pendingPermission(ch chan PermissionResponse) (PermissionResponse, bool) {
	select {
	case r := <-ch:
		return r, true
	default:
		return PermissionResponse{}, false
	}
}

// "/plan off" has an a in it: typed into the permission modal, it used to
// allow the call once. Typed text must not answer it.
func TestPermissionPromptTypedTextDoesNotAllow(t *testing.T) {
	for _, text := range []string{"/plan off", "/plan always", "hello Dan"} {
		ch := make(chan PermissionResponse, 1)
		m := openPermission(ch, 100)
		m = typePermission(m, text)
		require.True(t, m.Active(), "%q answered the prompt", text)
		assert.Contains(t, m.View(), "Typing is ignored here", text)
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		_, ok := pendingPermission(ch)
		assert.False(t, ok, "%q + Enter answered the prompt", text)
		assert.NotContains(t, m.View(), "Typing is ignored here", "Enter clears the hint")
		// After Enter the keys are the prompt's again.
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
		r, ok := pendingPermission(ch)
		require.True(t, ok)
		assert.Equal(t, "deny", r.Decision)
	}
}

func TestPermissionPromptKeysStillAnswer(t *testing.T) {
	for key, want := range map[rune]string{'a': "allow_once", 'A': "always_allow", 'd': "deny", 'D': "always_deny"} {
		ch := make(chan PermissionResponse, 1)
		m := openPermission(ch, 100)
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		r, ok := pendingPermission(ch)
		require.True(t, ok)
		assert.Equal(t, want, r.Decision)
	}
	// Enter and Esc: Enter picks nothing, Esc denies.
	ch := make(chan PermissionResponse, 1)
	m := openPermission(ch, 100)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, ok := pendingPermission(ch)
	assert.False(t, ok, "Enter must not pick a default")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	r, _ := pendingPermission(ch)
	assert.Equal(t, "deny", r.Decision)
}

// A paste containing "A" is typing, never "always allow".
func TestPermissionPromptPasteDoesNotAnswer(t *testing.T) {
	ch := make(chan PermissionResponse, 1)
	m := openPermission(ch, 100)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A"), Paste: true})
	_, ok := pendingPermission(ch)
	assert.False(t, ok)
	assert.True(t, m.Active())
}

// The box is closed: every row is as wide as the terminal and the right
// border is drawn on each one.
func TestPermissionPromptBorderIsClosed(t *testing.T) {
	for _, w := range []int{50, 80, 120} {
		m := openPermission(make(chan PermissionResponse, 1), w)
		m.inputSummary = strings.Repeat("long-argument ", 20)
		lines := strings.Split(m.View(), "\n")
		require.GreaterOrEqual(t, len(lines), 5)
		for i, l := range lines {
			assert.Equal(t, w, lipgloss.Width(l), "width %d row %d: %q", w, i, l)
		}
		plain := func(s string) string { return strings.TrimSpace(stripANSI(s)) }
		assert.True(t, strings.HasPrefix(plain(lines[0]), "╭") && strings.HasSuffix(plain(lines[0]), "╮"))
		assert.True(t, strings.HasPrefix(plain(lines[len(lines)-1]), "╰") && strings.HasSuffix(plain(lines[len(lines)-1]), "╯"))
		for _, l := range lines[1 : len(lines)-1] {
			assert.True(t, strings.HasPrefix(plain(l), "│") && strings.HasSuffix(plain(l), "│"), "row %q", plain(l))
		}
	}
}

var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func stripANSI(s string) string { return ansiSeq.ReplaceAllString(s, "") }
