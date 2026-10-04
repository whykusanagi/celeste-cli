package tui

import (
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The input's size limits (#358). The textarea would cut anything past
// its own CharLimit, and every row past its line cap, without a word; so
// its CharLimit is off and the input checks each insertion here first:
// text that does not fit is refused with a notice, never cut silently.
const (
	// inputCharLimit caps the input at 256K characters: room for a long
	// log or source file, far above what one turn usually needs.
	inputCharLimit = 256 * 1024
	// minInputCharLimit is the floor when the context window bounds the
	// limit: a 16 KB paste always fits.
	minInputCharLimit = 16 * 1024
	// inputCharsPerToken turns a context window into characters, at the
	// usual four characters a token.
	inputCharsPerToken = 4
	// maxInputLines is the textarea's own row cap (its maxLines): rows
	// past it would be dropped silently.
	maxInputLines = 10000
	// inputTabWidth is what the textarea turns a tab into.
	inputTabWidth = 4
	// pasteRestoreGap is the pause that ends an unbracketed paste for
	// the overflow rollback. It is far wider than burstWindow: while a
	// long paste lands, rendering the input can take longer than
	// burstWindow, so the gap between two keys of one paste, measured
	// when each is processed, can exceed it. A person pausing between
	// keys takes longer than this.
	pasteRestoreGap = 500 * time.Millisecond
)

var inputNoticeStyle = lipgloss.NewStyle().Foreground(ColorWarning)

// clipboardPasteMsg carries the clipboard text Ctrl+V read, so it goes
// through the input's limit instead of the textarea's silent cut.
type clipboardPasteMsg struct {
	text string
	err  error
}

func readClipboard() tea.Msg {
	s, err := clipboard.ReadAll()
	return clipboardPasteMsg{text: s, err: err}
}

// inputLimitFor is the input's character limit for a context window of
// tokens (0: unknown): the cap, or less when the window cannot hold it,
// never below the floor.
func inputLimitFor(tokens int) int {
	if tokens <= 0 || tokens >= inputCharLimit/inputCharsPerToken {
		return inputCharLimit
	}
	return max(tokens*inputCharsPerToken, minInputCharLimit)
}

// SetContextWindow bounds the input's character limit by the current
// model's context window, in tokens (0 when unknown).
func (m InputModel) SetContextWindow(tokens int) InputModel {
	m.charLimit = inputLimitFor(tokens)
	return m
}

// CharLimit returns the input's character limit.
func (m InputModel) CharLimit() int {
	if m.charLimit <= 0 {
		return inputCharLimit
	}
	return m.charLimit
}

// Notice returns the notice shown under the input ("" when none): why
// the last paste or key was not inserted.
func (m InputModel) Notice() string {
	return m.notice
}

// insertedRunes returns the text k inserts, or nil when it inserts none.
// An Alt key (alt+b, alt+f: move by a word) inserts nothing.
func insertedRunes(k tea.KeyMsg) []rune {
	if k.Alt && !isTextBurst(k) {
		return nil
	}
	switch k.Type {
	case tea.KeyRunes:
		return k.Runes
	case tea.KeySpace:
		return []rune{' '}
	}
	return nil
}

// inputRender holds the input's last rendered view. It is shared by the
// model's copies, so View (a value method) can keep it.
type inputRender struct {
	view string
	ok   bool
}

// inputRedrawMsg ends a deferred render: the input renders afresh.
type inputRedrawMsg struct{}

// deferRender lets View show the last render while a burst lands, and
// returns the command that ends it (nil when one is on its way).
// Rendering re-wraps the whole input, and Bubble Tea renders after
// every message: an unbracketed paste, one message per word, would pay
// that thousands of times over (#358). The redraw comes a burst window
// later, so a long paste repaints a few times a second while it lands
// and once it is in.
func (m *InputModel) deferRender() tea.Cmd {
	m.deferView = true
	if m.redrawPending {
		return nil
	}
	m.redrawPending = true
	return tea.Tick(burstWindow, func(time.Time) tea.Msg { return inputRedrawMsg{} })
}

// Bursting reports whether a burst of keys is landing and its redraw
// is still to come.
func (m InputModel) Bursting() bool {
	return m.deferView
}

// suggestionScanLimit: past this many characters the input is no slash
// command, so suggestions are not looked for (that would read the whole
// input on every key of a long paste).
const suggestionScanLimit = 1024

// refreshSuggestions recomputes the command suggestions for the input,
// counting it when its size is unknown.
func (m *InputModel) refreshSuggestions() {
	if m.used < 0 {
		value := m.textArea.Value()
		m.used = utf8.RuneCountInString(value)
		m.suggestions = computeSuggestions(value)
	} else if m.used > suggestionScanLimit {
		m.suggestions = nil
	} else {
		m.suggestions = computeSuggestions(m.textArea.Value())
	}
	if m.suggestionIdx >= len(m.suggestions) {
		m.suggestionIdx = 0
	}
}

// startsBurst reports whether an inserting key read at now, after the
// key read at prev (zero: none), starts a new burst: the input it lands
// in is the one a rollback restores. Typed keys a burst window apart are
// separate bursts, so a paste that overflows never takes typed text with
// it. Once a burst has carried a paste, only a pause of pasteRestoreGap
// ends it: render time can stretch the gaps inside a paste past the
// burst window (#358).
func (m *InputModel) startsBurst(prev, now time.Time) bool {
	if !m.burstBaseOK || prev.IsZero() {
		return true
	}
	gap := now.Sub(prev)
	if m.burstPaste {
		return gap >= pasteRestoreGap
	}
	return gap >= burstWindow
}

// markBurstBase remembers the input and its cursor as the start of a
// burst of keys.
func (m *InputModel) markBurstBase() {
	li := m.textArea.LineInfo()
	m.burstBase, m.burstBaseOK = m.textArea.Value(), true
	m.burstRow, m.burstCol = m.textArea.Line(), li.StartColumn+li.ColumnOffset
	m.burstPaste = false
}

// restoreBurstBase puts the input and its cursor back to the start of
// the burst.
func (m *InputModel) restoreBurstBase() {
	m.textArea.SetValue(m.burstBase) // the cursor ends up at the end
	for m.textArea.Line() > m.burstRow {
		m.textArea.CursorUp()
	}
	m.textArea.SetCursor(m.burstCol)
	m.used = -1
}

// valueReplaced records that the input was set or cleared other than by
// an insertion: its size is unknown until counted, and no burst runs on.
func (m *InputModel) valueReplaced() {
	m.used = -1
	m.burstBaseOK = false
}

// admit reports whether runes fit in the input. m.used is an upper
// bound kept by the insertions (a deletion only makes it high), so the
// input is counted only when the bound says the text might not fit:
// counting on every key would make a long unbracketed paste, which
// arrives as one message per word, quadratic.
//
// Text that does not fit is not inserted and the notice says why. Keys
// of a burst that carried several runes in one message are an
// unbracketed paste: the part of it already inserted is taken back out
// (the input and cursor return to where they were before the burst) and
// the rest of the burst is dropped, so a paste either lands whole or not
// at all, as a bracketed one does, and there is room for what the user
// types next. A single typed key that does not fit is refused alone.
func (m *InputModel) admit(runes []rune, bracketed bool) bool {
	limit := m.CharLimit()
	adding, newlines := 0, 0
	for _, r := range runes {
		switch r {
		case '\t':
			adding += inputTabWidth
		case '\r', '\n':
			newlines++
			adding++
		default:
			adding++
		}
	}
	lines := m.textArea.LineCount() + newlines
	used := m.used
	if used < 0 || used+adding > limit {
		used = utf8.RuneCountInString(m.textArea.Value())
	}
	if lines <= maxInputLines && used+adding <= limit {
		m.used = used + adding
		return true
	}
	m.used = used

	paste := bracketed || len(runes) > 1 || (m.burstBaseOK && m.burstPaste)
	switch {
	case !paste:
		m.notice = fmt.Sprintf("Input full: it holds %s characters.", commaInt(limit))
	case lines > maxInputLines:
		m.notice = fmt.Sprintf("Paste not inserted: it would make %s lines, over the %s-line input limit.",
			commaInt(lines), commaInt(maxInputLines))
	default:
		m.notice = fmt.Sprintf("Paste not inserted: it is over the %s-character input limit. Save long text to a file and point to it instead.",
			commaInt(limit))
	}
	if paste && !bracketed {
		// Every inserting key marks a burst base first when none is
		// set, so there is always one to restore here.
		m.restoreBurstBase()
		m.burstBaseOK = false
		m.overflowAt = keyClock()
	}
	return false
}

// clearNotice drops the notice and the overflow, once the input is sent
// or cleared.
func (m *InputModel) clearNotice() {
	m.notice = ""
	m.overflowAt = time.Time{}
	m.valueReplaced()
}

// commaInt formats n with thousands separators: 262,144.
func commaInt(n int) string {
	if n < 0 {
		return "-" + commaInt(-n)
	}
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
