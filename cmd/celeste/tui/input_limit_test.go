package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pasteText is n characters of multi-line text, the shape of a pasted
// log or source file.
func pasteText(n int) string {
	line := "0123456789 the quick brown fox jumps over the lazy dog 0123456789\n"
	s := strings.Repeat(line, n/len(line)+1)
	return s[:n]
}

// flatText is n characters on one line: an unbracketed paste carries
// no newline as a rune (the terminal sends it as Enter or Ctrl+J).
func flatText(n int) string {
	return strings.ReplaceAll(pasteText(n), "\n", " ")
}

// bracketed is a paste from a terminal with bracketed paste: one message.
func bracketed(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

// unbracketed splits s into the messages a terminal without bracketed
// paste delivers: reads of at most size bytes, each parsed as burst()
// does.
func unbracketed(s string, size int) []tea.KeyMsg {
	var out []tea.KeyMsg
	for len(s) > 0 {
		n := min(size, len(s))
		out = append(out, burst(s[:n])...)
		s = s[n:]
	}
	return out
}

func typeInput(m InputModel, s string) InputModel {
	for _, r := range s {
		if r == ' ' {
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			continue
		}
		m, _ = m.Update(runeKey(r))
	}
	return m
}

// #358: a 16 KB bracketed paste lands whole, and text typed after it
// is kept.
func TestInputBracketedPasteThenTypingKept(t *testing.T) {
	humanPace(t)
	paste := pasteText(16 * 1024)
	m := NewInputModel().Focus()
	m, _ = m.Update(bracketed(paste))
	m = typeInput(m, " and then this")
	assert.Equal(t, paste+" and then this", m.Value())
	assert.Empty(t, m.Notice())
}

// #358: the same paste from a terminal without bracketed paste.
func TestInputUnbracketedPasteThenTypingKept(t *testing.T) {
	move := stoppedKeyClock(t)
	paste := flatText(16 * 1024)
	m := NewInputModel().Focus()
	for _, k := range unbracketed(paste, 256) {
		m, _ = m.Update(k)
	}
	move(time.Second)
	m = typeInput(m, "tail")
	assert.Equal(t, paste+"tail", m.Value())
	assert.Empty(t, m.Notice())
}

// The limit is far above the old 4096 characters.
func TestInputLimitIsLarge(t *testing.T) {
	m := NewInputModel()
	assert.Equal(t, inputCharLimit, m.CharLimit())
	assert.GreaterOrEqual(t, inputCharLimit, 256*1024)
}

// A bracketed paste over the limit is not cut silently: nothing of it
// is inserted, a notice says why, and typing still works.
func TestInputOversizeBracketedPasteNotice(t *testing.T) {
	humanPace(t)
	m := NewInputModel().Focus()
	m = typeInput(m, "before ")
	m, _ = m.Update(bracketed(pasteText(inputCharLimit + 1)))
	assert.Equal(t, "before ", m.Value(), "an oversize paste must not be inserted in part")
	require.NotEmpty(t, m.Notice())
	assert.Contains(t, m.Notice(), "limit")
	assert.Contains(t, m.View(), m.Notice(), "the notice is shown under the input")

	m = typeInput(m, "after")
	assert.Equal(t, "before after", m.Value())
}

// An unbracketed paste over the limit arrives as many messages: once
// one does not fit, the part already inserted is taken back out (the
// input is what it was before the paste), the rest of the burst is
// dropped with a notice, and text typed afterwards is kept.
func TestInputOversizeUnbracketedPasteNotice(t *testing.T) {
	move := stoppedKeyClock(t)
	paste := flatText(inputCharLimit + 50_000)
	m := NewInputModel().Focus().SetValue("before ")
	start := time.Now()
	for _, k := range unbracketed(paste, 1000) {
		m, _ = m.Update(k)
	}
	assert.Less(t, time.Since(start), 30*time.Second, "a long unbracketed paste must not be quadratic")
	require.NotEmpty(t, m.Notice())
	assert.Contains(t, m.Notice(), "limit")
	assert.Equal(t, "before ", m.Value())

	move(time.Second)
	m = typeInput(m, "typed")
	assert.Equal(t, "before typed", m.Value())
}

// An unbracketed paste whose Enter comes after the overflow does not
// send the cut text: the rest of the burst, Enter included, is dropped.
func TestInputOversizeUnbracketedPasteDropsEnter(t *testing.T) {
	move := stoppedKeyClock(t)
	m := NewInputModel().Focus().SetContextWindow(1)
	keys := unbracketed(flatText(m.CharLimit()+5_000), 500)
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd, "nothing is sent")
	assert.Equal(t, "", m.Value())
	move(time.Second)
	m = typeInput(m, "ok")
	assert.Equal(t, "ok", m.Value())
}

// A typed key at a full input says so instead of vanishing.
func TestInputFullTypedKeyNotice(t *testing.T) {
	humanPace(t)
	m := NewInputModel().Focus()
	m = m.SetContextWindow(1) // the floor
	m, _ = m.Update(bracketed(strings.Repeat("a", m.CharLimit())))
	require.Empty(t, m.Notice())
	m = typeInput(m, "b")
	assert.Equal(t, strings.Repeat("a", m.CharLimit()), m.Value())
	assert.NotEmpty(t, m.Notice())
}

// The textarea drops rows past its line cap silently; a paste with more
// lines than that is refused with a notice instead.
func TestInputTooManyLinesNotice(t *testing.T) {
	humanPace(t)
	m := NewInputModel().Focus()
	m, _ = m.Update(bracketed(strings.Repeat("x\n", maxInputLines+1)))
	assert.Equal(t, "", m.Value())
	assert.Contains(t, m.Notice(), "lines")
}

// The limit is bounded by the model's context window (about four
// characters a token), never below the floor nor above the cap.
func TestInputLimitBoundedByContextWindow(t *testing.T) {
	m := NewInputModel()
	assert.Equal(t, 32_000, m.SetContextWindow(8_000).CharLimit())
	assert.Equal(t, inputCharLimit, m.SetContextWindow(0).CharLimit())
	assert.Equal(t, inputCharLimit, m.SetContextWindow(10_000_000).CharLimit())
	assert.Equal(t, minInputCharLimit, m.SetContextWindow(10).CharLimit())

	humanPace(t)
	m = m.SetContextWindow(8_000).Focus()
	m, _ = m.Update(bracketed(pasteText(40_000)))
	assert.Equal(t, "", m.Value())
	assert.Contains(t, m.Notice(), "32,000")
}

// Ctrl+V reads the clipboard through the same limit (the textarea's own
// paste would cut it silently).
func TestInputClipboardPasteUsesLimit(t *testing.T) {
	m := NewInputModel().Focus()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	require.NotNil(t, cmd, "ctrl+v reads the clipboard")

	m, _ = m.Update(clipboardPasteMsg{text: pasteText(16 * 1024)})
	assert.Equal(t, pasteText(16*1024), m.Value())

	m, _ = m.Update(clipboardPasteMsg{text: pasteText(inputCharLimit)})
	assert.Equal(t, pasteText(16*1024), m.Value())
	assert.NotEmpty(t, m.Notice())
}

// Sending or clearing the input clears the notice.
func TestInputNoticeClearedOnSend(t *testing.T) {
	humanPace(t)
	m := NewInputModel().Focus()
	m, _ = m.Update(bracketed(pasteText(inputCharLimit + 1)))
	require.NotEmpty(t, m.Notice())
	m = typeInput(m, "hi")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Empty(t, m.Notice())

	m, _ = m.Update(bracketed(pasteText(inputCharLimit + 1)))
	require.NotEmpty(t, m.Notice())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Empty(t, m.Notice())
}

// #358 through the chat: a 16 KB paste and the text typed after it both
// reach the input.
func TestAppLongPasteThenTypingKept(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	humanPace(t)
	paste := pasteText(16 * 1024)
	var m tea.Model = NewApp(nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = m.Update(bracketed(paste))
	for _, r := range "tail" {
		m, _ = m.Update(runeKey(r))
	}
	assert.Equal(t, paste+"tail", m.(AppModel).input.Value())
}

// The clipboard text Ctrl+V read reaches the input from the app.
func TestAppClipboardPasteReachesInput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var m tea.Model = NewApp(nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = m.Update(clipboardPasteMsg{text: "from the clipboard"})
	assert.Equal(t, "from the clipboard", m.(AppModel).input.Value())
}

// While an unbracketed paste lands the input shows its last render (a
// render re-wraps the whole input, once per message); the redraw that
// follows the burst shows the text.
func TestInputBurstDefersRender(t *testing.T) {
	move := stoppedKeyClock(t)
	m := NewInputModel().Focus().SetWidth(100)
	before := m.View()
	var redraw tea.Cmd
	for _, k := range unbracketed("one two three four", 6) {
		var cmd tea.Cmd
		m, cmd = m.Update(k)
		if cmd != nil {
			require.Nil(t, redraw, "one redraw at a time")
			redraw = cmd
		}
	}
	require.NotNil(t, redraw)
	assert.Equal(t, before, m.View(), "no re-render mid-burst")
	move(time.Second)
	m, _ = m.Update(inputRedrawMsg{})
	assert.Contains(t, m.View(), "one two three four")
}

// Deleting a long input back down to a slash command brings the
// suggestions back (the size bound is recounted after a deletion).
func TestInputSuggestionsAfterDeletingLongText(t *testing.T) {
	humanPace(t)
	m := NewInputModel().Focus()
	m = typeInput(m, "/ind")
	m, _ = m.Update(bracketed(strings.Repeat("x", 2*suggestionScanLimit)))
	assert.False(t, m.HasSuggestions())
	for range 2 * suggestionScanLimit {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	assert.Equal(t, "/ind", m.Value())
	assert.True(t, m.HasSuggestions())
}

// renderStretchedPaste feeds keys to m the way a real terminal paces
// them while the input renders: a key whose message leaves the render
// deferred costs a millisecond, but a full render (after a redraw, or
// a key the textarea handles) takes 60 ms, longer than the burst
// window. The redraw tick fires every 20 keys while one is pending.
func renderStretchedPaste(m InputModel, move func(time.Duration), keys []tea.KeyMsg) InputModel {
	pending := false
	for i, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(k)
		if cmd != nil {
			pending = true
		}
		if pending && i%20 == 19 {
			m, _ = m.Update(inputRedrawMsg{})
			pending = false
		}
		if m.Bursting() {
			move(time.Millisecond)
		} else {
			move(60 * time.Millisecond)
		}
	}
	return m
}

// An oversize unbracketed paste on a clock that render time stretches
// past the burst window is still taken out whole, and its Enter does
// not send what was left of it.
func TestInputOversizeUnbracketedPasteRenderStretched(t *testing.T) {
	move := stoppedKeyClock(t)
	m := NewInputModel().Focus().SetContextWindow(1).SetValue("before ")
	paste := flatText(m.CharLimit() + 5_000)
	m = renderStretchedPaste(m, move, unbracketed(paste, 500))
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd, "nothing of the paste is sent")
	assert.Equal(t, "before ", m.Value(), "the input is what it was before the paste")
	assert.Contains(t, m.Notice(), "not inserted")

	move(time.Second)
	m = typeInput(m, "typed")
	assert.Equal(t, "before typed", m.Value())
}

// A fitting unbracketed paste on the same stretched clock lands whole.
func TestInputUnbracketedPasteRenderStretchedKept(t *testing.T) {
	move := stoppedKeyClock(t)
	m := NewInputModel().Focus()
	paste := flatText(8 * 1024)
	m = renderStretchedPaste(m, move, unbracketed(paste, 500))
	assert.Equal(t, paste, m.Value())
	assert.Empty(t, m.Notice())
}

// Keys typed fast at a nearly full input are not a paste: the ones that
// fit are kept, the one that does not is refused as a key, and Enter
// right after it still sends.
func TestInputFastTypingAtLimitKeepsFittingKeys(t *testing.T) {
	move := stoppedKeyClock(t)
	m := NewInputModel().Focus().SetContextWindow(1)
	full := strings.Repeat("a", m.CharLimit()-3)
	m = m.SetValue(full)
	move(time.Second)
	for _, r := range "xyzw" {
		m, _ = m.Update(runeKey(r))
		move(30 * time.Millisecond)
	}
	assert.Equal(t, full+"xyz", m.Value())
	assert.Contains(t, m.Notice(), "Input full")
	move(30 * time.Millisecond)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotNil(t, cmd, "Enter sends what was typed")
}

// Alt+letter moves the cursor by a word; it inserts nothing, so a full
// input does not refuse it.
func TestInputAltKeyNotCountedAsText(t *testing.T) {
	humanPace(t)
	m := NewInputModel().Focus().SetContextWindow(1)
	m = m.SetValue(strings.Repeat("a", m.CharLimit()-5) + " word")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true})
	assert.Empty(t, m.Notice(), "alt+b is not an insertion")
	assert.Equal(t, strings.Repeat("a", m.CharLimit()-5)+" word", m.Value())
}

// A refused unbracketed paste puts the cursor back where it was.
func TestInputOversizeUnbracketedPasteRestoresCursor(t *testing.T) {
	move := stoppedKeyClock(t)
	m := NewInputModel().Focus().SetContextWindow(1).SetValue("first line\nsecond line")
	m.textArea.CursorUp()
	m.textArea.SetCursor(5)
	move(time.Second)
	for _, k := range unbracketed(flatText(m.CharLimit()+100), 500) {
		m, _ = m.Update(k)
	}
	require.Equal(t, "first line\nsecond line", m.Value())
	assert.Equal(t, 0, m.textArea.Line())
	assert.Equal(t, 5, m.textArea.LineInfo().ColumnOffset)
}
