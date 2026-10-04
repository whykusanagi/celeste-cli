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
func insertedRunes(k tea.KeyMsg) []rune {
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
// Text that does not fit is not inserted and the notice says why. A
// key inside a burst (inPaste) is part of an unbracketed paste: the part
// of it already inserted is taken back out (the input returns to what it
// was before the burst) and the rest of the burst is dropped, so a paste
// either lands whole or not at all, as a bracketed one does, and there
// is room for what the user types next.
func (m *InputModel) admit(runes []rune, bracketed, inPaste bool) bool {
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

	paste := bracketed || inPaste || len(runes) > 1
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
	if !bracketed {
		if paste && m.burstBaseOK {
			m.textArea.SetValue(m.burstBase)
			m.used = -1
		} else if paste {
			m.notice = fmt.Sprintf("Paste too long: the input holds %s characters; the rest of the paste was dropped.",
				commaInt(limit))
		}
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
