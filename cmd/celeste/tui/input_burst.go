package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// isTextBurst reports whether k is several printable characters in one
// message: text typed or pasted faster than one key per read, as a
// terminal without bracketed paste delivers it (#320). Its String() is
// those characters, so the word "left" would match the Left key's name;
// it must be handled as text, never as a named key. A bracketed paste is
// not one: its String() is wrapped in brackets and cannot collide.
func isTextBurst(k tea.KeyMsg) bool {
	return k.Type == tea.KeyRunes && !k.Paste && len(k.Runes) > 1
}

// keyName is k's name for a switch over named keys: k.String(), or ""
// for a text burst, which names no key.
func keyName(k tea.KeyMsg) string {
	if isTextBurst(k) {
		return ""
	}
	return k.String()
}

// typeEach feeds a text burst to the app one character at a time, the
// way it arrives when typed: for a view whose text field reads single
// keys (graph search).
func (m AppModel) typeEach(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var model tea.Model = m
	for _, r := range k.Runes {
		app, ok := model.(AppModel)
		if !ok {
			break
		}
		var cmd tea.Cmd
		model, cmd = app.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: k.Alt})
		cmds = append(cmds, cmd)
	}
	return model, tea.Batch(cmds...)
}

// burstWindow: keys closer together than this came in one input burst,
// a paste on a terminal without bracketed paste. Nobody presses a key
// and then Enter that fast, so a modal never takes such an Enter as a
// confirmation (#326).
const burstWindow = 50 * time.Millisecond

// keyClock reads the time a key arrived (tests replace it).
var keyClock = time.Now

// inBurst reports whether a key arriving now came in the same burst as
// the key read at prev (zero: none).
func inBurst(prev time.Time) bool {
	return !prev.IsZero() && keyClock().Sub(prev) < burstWindow
}
