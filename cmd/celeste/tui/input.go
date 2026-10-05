// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the input component with command history.
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// knownCommands is the authoritative list of slash commands for typeahead.
var knownCommands = []string{
	"agent", "agents", "clear", "collections", "compact", "config", "confirm",
	"context", "costs", "diff", "effort", "endpoint", "export", "fork", "graph",
	"grimoire", "handoff", "help", "image-model", "index", "init", "list-models",
	"mcp", "memories", "menu", "model", "nsfw", "orch", "orchestrate", "persona",
	"plan", "providers", "rewind", "safe", "session", "set-model", "skills",
	"stats", "tools", "undo", "user", "voice",
}

var (
	suggestionTabStyle    = lipgloss.NewStyle().Foreground(ColorTextMuted)
	suggestionActiveStyle = lipgloss.NewStyle().Foreground(ColorPurpleNeon).Bold(true)
	suggestionDimStyle    = lipgloss.NewStyle().Foreground(ColorTextMuted)
)

// knownSubcommands maps parent commands to their valid subcommands.
var knownSubcommands = map[string][]string{
	"index":   {"rebuild", "update", "snapshot", "diff", "impact"},
	"config":  {"set-key", "set-model", "set-url"},
	"voice":   {"list", "set-key", "set-voice"},
	"user":    {"reset"},
	"session": {"new", "resume", "list", "clear", "merge", "info", "rename", "delete"},
	"agent":   {"list-runs", "resume"}, // kill lives on /agents (W-A1)
	"agents":  {"resume", "kill"},
	"plan":    {"off", "show", "help"},
	"init":    {"agents"},
}

// computeSuggestions returns commands or subcommands that match the input.
// For "/ind" → suggests "/index". For "/index " → suggests subcommands.
func computeSuggestions(value string) []string {
	if !strings.HasPrefix(value, "/") {
		return nil
	}
	raw := strings.TrimPrefix(value, "/")
	if raw == "" {
		return nil
	}

	parts := strings.SplitN(raw, " ", 2)
	cmd := parts[0]

	// If there's a space, we're in subcommand territory
	if len(parts) == 2 {
		subPartial := strings.TrimSpace(parts[1])
		subs, ok := knownSubcommands[cmd]
		if !ok {
			return nil
		}
		// Show matching subcommands (or all if partial is empty)
		var matches []string
		for _, sub := range subs {
			if subPartial == "" || (strings.HasPrefix(sub, subPartial) && sub != subPartial) {
				matches = append(matches, cmd+" "+sub)
			}
		}
		return matches
	}

	// First word: match top-level commands
	var matches []string
	for _, known := range knownCommands {
		if strings.HasPrefix(known, cmd) && known != cmd {
			matches = append(matches, known)
		}
	}
	return matches
}

// InputModel wraps a textarea for multi-line input with word wrap,
// command history, and slash-command typeahead.
type InputModel struct {
	textArea      textarea.Model
	width         int
	history       []string
	historyIndex  int
	tempInput     string    // Stores current input when browsing history
	suggestions   []string  // Current typeahead matches
	suggestionIdx int       // Which suggestion is highlighted (Tab cycles)
	charLimit     int       // Character limit (0: inputCharLimit), see input_limit.go
	notice        string    // Why the last paste or key was not inserted
	overflowAt    time.Time // When a burst last overflowed the limit (#358)
	lastKeyAt     time.Time // When the last key arrived
	used          int       // Characters in the input, an upper bound (-1: unknown)
	burstBase     string    // The input before the current burst of keys
	burstBaseOK   bool      // burstBase is the input this burst started from
	burstRow      int       // The cursor's row at burstBase
	burstCol      int       // The cursor's column at burstBase
	burstPaste    bool      // A key of this burst carried several runes: a paste
	deferView     bool      // A burst is landing: View may show the last render
	redrawPending bool      // An inputRedrawMsg is on its way
	rendered      *inputRender
	// The held tail of an overflowed paste (K3), see input_tail.go.
	tailArmed   bool      // No key has arrived since the overflow's burst ended
	tailOpen    bool      // The burst's last key was not a rune run
	tailPending []rune    // The first rune run after the burst, held to settle
	tailSpaces  int       // Spaces that came right after tailPending, held too
	tailAt      time.Time // When the last held key arrived
	tailSeq     int       // Which settle tick is the held keys'
}

// NewInputModel creates a new input model using textarea for word-wrap support.
func NewInputModel() InputModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message or 'help'..."
	ta.Focus()
	// No textarea CharLimit: it cuts silently. The input checks every
	// insertion against its own limit and says when text does not fit
	// (#358, input_limit.go).
	ta.CharLimit = 0
	// Ctrl+V reads the clipboard through that limit too.
	ta.KeyMap.Paste.SetEnabled(false)
	ta.SetWidth(80)
	ta.SetHeight(3) // 3 visible lines — expands visually with wrapping
	ta.ShowLineNumbers = false
	ta.Prompt = "❯ "
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle() // No highlight on current line
	ta.FocusedStyle.Base = InputTextStyle
	ta.FocusedStyle.Placeholder = InputPlaceholderStyle
	ta.FocusedStyle.Prompt = InputPromptStyle
	ta.BlurredStyle = ta.FocusedStyle
	// Use soft wrap so long lines wrap instead of scrolling horizontally
	ta.KeyMap.InsertNewline.SetEnabled(false) // Enter sends, not inserts newline

	return InputModel{
		textArea:     ta,
		history:      []string{},
		historyIndex: -1,
		charLimit:    inputCharLimit,
		rendered:     &inputRender{},
	}
}

// SetWidth sets the input width.
func (m InputModel) SetWidth(width int) InputModel {
	if width < 20 {
		width = 80
	}
	m.width = width
	m.textArea.SetWidth(width - 4) // Account for borders/padding
	m.deferView = false
	return m
}

// Value returns the current input value.
func (m InputModel) Value() string {
	return m.textArea.Value()
}

// SetValue sets the input text.
func (m InputModel) SetValue(s string) InputModel {
	m.textArea.SetValue(s)
	m.valueReplaced()
	m.deferView = false
	return m
}

// HasSuggestions reports whether the command suggestion list is showing.
func (m InputModel) HasSuggestions() bool {
	return len(m.suggestions) > 0
}

// Focus gives focus to the input.
func (m InputModel) Focus() InputModel {
	m.textArea.Focus()
	return m
}

// Init implements the init method for InputModel.
func (m InputModel) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles messages for the input component.
func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case clipboardPasteMsg:
		if msg.err != nil {
			m.notice = "Clipboard unreadable: " + msg.err.Error()
			return m, nil
		}
		m.deferView = false
		if !m.admit([]rune(msg.text), true) {
			return m, nil
		}
		m.textArea.InsertString(msg.text)
		m.textArea, cmd = m.textArea.Update(msg) // scroll to the cursor
		m.refreshSuggestions()
		return m, cmd

	case inputRedrawMsg:
		// The burst is over (or a frame's worth of it landed): render
		// afresh, the textarea scrolled to the cursor.
		m.redrawPending = false
		m.deferView = false
		m.textArea, cmd = m.textArea.Update(msg)
		return m, cmd

	case tailSettleMsg:
		if msg.seq == m.tailSeq {
			m.settleTailQuiet()
		}
		return m, nil

	case tea.KeyMsg:
		var held bool
		if m, held, cmd = m.settleTail(msg); held {
			return m, cmd
		}
		m.deferView = false
		now := keyClock()
		prevKeyAt := m.lastKeyAt
		inPaste := inBurst(prevKeyAt) // this key came in the same burst
		m.lastKeyAt = now
		if !m.overflowAt.IsZero() && now.Sub(m.overflowAt) < pasteRestoreGap {
			// The rest of a paste that did not fit: dropped whole,
			// Enter included, so no fragment of it lands or is sent.
			m.overflowAt = now
			m.tailOpen = !plainRuneRun(msg)
			return m, nil
		}
		if m.tailArmed {
			// The first key after an overflowed burst may carry the
			// burst's last word, held back by the terminal reader (K3).
			m.tailArmed = false
			if m.tailOpen && plainRuneRun(msg) {
				return m, m.holdTail(msg.Runes, now)
			}
		}
		runes := insertedRunes(msg)
		if runes != nil && !msg.Paste {
			if m.startsBurst(prevKeyAt, now) {
				// A key that may start an unbracketed paste: remember
				// the input it started from, to restore if the paste
				// overflows.
				m.markBurstBase()
			}
			if len(runes) > 1 {
				m.burstPaste = true
			}
		}
		if runes != nil && !m.admit(runes, msg.Paste) {
			return m, nil
		}
		if isTextBurst(msg) || (runes != nil && !msg.Paste && !msg.Alt && inPaste) {
			// Text, whatever it spells: the word "left" is not the Left
			// key (#320), so it skips the textarea's key bindings too.
			// So does any key inside the burst: the textarea re-wraps
			// the whole row on every key it handles, which a long
			// unbracketed paste would pay once per word (#358).
			m.textArea.InsertString(string(runes))
			m.refreshSuggestions()
			return m, m.deferRender()
		}
		switch msg.String() {
		case "ctrl+v":
			return m, readClipboard

		case "tab":
			// Complete with the highlighted suggestion
			if len(m.suggestions) > 0 {
				m.textArea.SetValue("/" + m.suggestions[m.suggestionIdx] + " ")
				m.valueReplaced()
				m.suggestions = nil
				m.suggestionIdx = 0
				return m, nil
			}
			// Otherwise Tab submits the input as a follow-up: during a turn it
			// waits for the reply instead of steering (#172).
			value := m.textArea.Value()
			if strings.TrimSpace(value) == "" {
				return m, nil
			}
			m.history = append(m.history, value)
			m.historyIndex = len(m.history)
			m.tempInput = ""
			m.textArea.Reset()
			m.clearNotice()
			return m, QueueFollowUp(value)

		case "enter":
			value := m.textArea.Value()
			if strings.TrimSpace(value) != "" {
				// Add to history
				m.history = append(m.history, value)
				m.historyIndex = len(m.history)
				m.tempInput = ""
				m.suggestions = nil
				m.suggestionIdx = 0

				// Clear input
				m.textArea.Reset()
				m.clearNotice()

				// Send message
				return m, SendMessage(value)
			}
			return m, nil

		case "up":
			// Browse history backwards (only when input is single line / at top)
			if len(m.textArea.Value()) == 0 || !strings.Contains(m.textArea.Value(), "\n") {
				if len(m.history) > 0 {
					if m.historyIndex == len(m.history) {
						m.tempInput = m.textArea.Value()
					}
					if m.historyIndex > 0 {
						m.historyIndex--
						m.textArea.SetValue(m.history[m.historyIndex])
						m.valueReplaced()
					}
				}
				return m, nil
			}

		case "down":
			// Browse history forwards (only when input is single line / at bottom)
			if len(m.textArea.Value()) == 0 || !strings.Contains(m.textArea.Value(), "\n") {
				if m.historyIndex < len(m.history) {
					m.historyIndex++
					if m.historyIndex == len(m.history) {
						m.textArea.SetValue(m.tempInput)
					} else {
						m.textArea.SetValue(m.history[m.historyIndex])
					}
					m.valueReplaced()
				}
				return m, nil
			}

		case "esc":
			// Clear input if non-empty, otherwise no-op
			if strings.TrimSpace(m.textArea.Value()) != "" {
				m.textArea.Reset()
				m.suggestions = nil
				m.suggestionIdx = 0
			}
			m.clearNotice()
			return m, nil

		case "ctrl+u":
			// Clear input line
			m.textArea.Reset()
			m.suggestions = nil
			m.clearNotice()
			return m, nil

		case "ctrl+w":
			// Delete last word
			val := m.textArea.Value()
			trimmed := strings.TrimRight(val, " ")
			lastSpace := strings.LastIndex(trimmed, " ")
			if lastSpace >= 0 {
				m.textArea.SetValue(val[:lastSpace+1])
			} else {
				m.textArea.Reset()
			}
			m.valueReplaced()
			return m, nil
		}
	}

	// Delegate to textarea for character input, cursor movement, etc.
	m.textArea, cmd = m.textArea.Update(msg)
	if k, ok := msg.(tea.KeyMsg); ok && insertedRunes(k) == nil {
		// A deletion or an edit: the size bound is stale until counted.
		m.valueReplaced()
	}

	// Update suggestions based on current value
	m.refreshSuggestions()

	return m, cmd
}

// View renders the input component.
func (m InputModel) View() string {
	if m.deferView && m.rendered != nil && m.rendered.ok {
		return m.rendered.view
	}
	inputView := m.textArea.View()

	// Render typeahead suggestions below input
	var hintLine string
	if len(m.suggestions) > 0 {
		hintLine = suggestionRow(m.suggestions, m.suggestionIdx, m.width)
	}

	if hintLine != "" {
		inputView += "\n" + hintLine
	}
	if m.notice != "" {
		// Wrapped, not cut, so the advice stays readable at 80 columns
		// (C1); continuation rows hang under the text.
		inputView += "\n" + inputNoticeStyle.Render(wrapText("  "+m.notice, m.width))
	}
	if m.rendered != nil {
		m.rendered.view, m.rendered.ok = inputView, true
	}
	return inputView
}

// suggestionRow lays the typeahead suggestions out on one row of at most
// width cells (0: unbounded). Whole names only: those that do not fit are
// left out and a "…" marks the side they were on, and the window starts
// late enough that the highlighted one is on screen.
func suggestionRow(suggestions []string, active, width int) string {
	const lead, sep, more = "  ", " · ", " …"
	names := make([]string, len(suggestions))
	for i, s := range suggestions {
		names[i] = "/" + s
	}
	fits := func(from, to int) bool { // names[from:to] with their marks
		w := lipgloss.Width(lead + strings.Join(names[from:to], sep))
		if from > 0 {
			w += lipgloss.Width("…" + sep)
		}
		if to < len(names) {
			w += lipgloss.Width(more)
		}
		return width <= 0 || w <= width
	}
	from, to := 0, len(names)
	if !fits(from, to) {
		// Grow a window from the first name, sliding it until the
		// highlighted name is in it.
		to = from + 1
		for to < len(names) && fits(from, to+1) {
			to++
		}
		for active >= to && to < len(names) {
			to++
			for from < active && !fits(from, to) {
				from++
			}
		}
	}
	parts := make([]string, 0, to-from+2)
	if from > 0 {
		parts = append(parts, suggestionDimStyle.Render("…"))
	}
	for i := from; i < to; i++ {
		if i == active {
			parts = append(parts, suggestionActiveStyle.Render(names[i]))
		} else {
			parts = append(parts, suggestionDimStyle.Render(names[i]))
		}
	}
	row := suggestionTabStyle.Render(lead) + strings.Join(parts, suggestionDimStyle.Render(sep))
	if to < len(names) {
		row += suggestionDimStyle.Render(more)
	}
	return row
}

// SetHistory sets the command history.
func (m InputModel) SetHistory(history []string) InputModel {
	m.history = history
	m.historyIndex = len(history)
	return m
}

// GetHistory returns the command history.
func (m InputModel) GetHistory() []string {
	return m.history
}
