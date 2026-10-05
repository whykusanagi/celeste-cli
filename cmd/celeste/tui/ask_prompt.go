package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AskPromptModel renders a structured question with selectable options and
// sends the user's answer back over the request's Response channel. It mirrors
// PermissionPromptModel: it stores the reply channel and pushes the result on
// a keypress.
type AskPromptModel struct {
	active      bool
	question    string
	options     []AskOption
	multiSelect bool
	selected    int
	checked     map[int]bool
	response    chan AskResponseMsg
	width       int
	// height caps the modal's rows (0: no cap). A question taller than the
	// rows left after the options and footer scrolls (PgUp/PgDn) from
	// offset, so the options never push its start off the screen.
	height int
	offset int
	// typed is set by a keystroke that is not one of the modal's keys
	// (typing meant for the input, a paste). While it is set the letter
	// keys are text too, the cursor is back on the first option and the
	// footer says how to answer; the next Enter only clears it, so typed
	// text plus Enter never answers. An arrow clears it too.
	typed bool
	// pickAt is when the last printable modal key (a number, j, k, a
	// space) arrived; zero after any other key. An Enter in the same
	// burst is part of a paste, not a choice (#326).
	pickAt time.Time
	// deadline is when the question expires (zero: no deadline shown).
	deadline time.Time
}

// askTypedHint replaces the footer while typed keys are being ignored.
const askTypedHint = "Typing is ignored here. Choose an option: ↑/↓ then Enter (Esc cancels)"

// NewAskPromptModel creates an inactive ask prompt.
func NewAskPromptModel() AskPromptModel {
	return AskPromptModel{checked: map[int]bool{}}
}

// Active reports whether a question is awaiting an answer.
func (m AskPromptModel) Active() bool { return m.active }

// SetSize sets the render width and the most rows the modal may take
// (0: no cap).
func (m *AskPromptModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.offset = min(m.offset, m.maxOffset())
}

func (m AskPromptModel) Update(msg tea.Msg) (AskPromptModel, tea.Cmd) {
	switch msg := msg.(type) {
	case AskRequestMsg:
		m.active = true
		m.question = msg.Question
		m.options = msg.Options
		m.multiSelect = msg.MultiSelect
		m.response = msg.Response
		m.selected = 0
		m.offset = 0
		m.typed = false
		m.pickAt = time.Time{}
		m.checked = map[int]bool{}
		m.deadline = msg.Deadline

	case tea.KeyMsg:
		if !m.active {
			break
		}
		if m.isTyping(msg) {
			// The first option is the safe default (callers put it there:
			// submit_plan's "Keep planning"), so typing never leaves the
			// cursor on anything else.
			m.typed = true
			m.selected = 0
			break
		}
		pickAt := m.pickAt
		m.pickAt = time.Time{}
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			// Stamped when Update handles the key, not when it arrived: if
			// the loop stalls and a real pick and its Enter are handled
			// back to back, the Enter reads as a paste and the user presses
			// it again. That fails safe.
			m.pickAt = keyClock() // a key a paste can contain
		}
		switch msg.String() {
		case "up", "k":
			m.typed = false
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			m.typed = false
			if m.selected < len(m.options)-1 {
				m.selected++
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// A number moves the cursor to that option; a later Enter answers.
			if n := int(msg.Runes[0] - '1'); n < len(m.options) {
				m.selected = n
			}
		case "pgdown":
			m.offset = min(m.offset+m.scrollStep(), m.maxOffset())
		case "pgup":
			m.offset = max(m.offset-m.scrollStep(), 0)
		case " ":
			if m.multiSelect {
				m.checked[m.selected] = !m.checked[m.selected]
			}
		case "enter":
			if m.typed {
				m.typed = false
				break
			}
			if inBurst(pickAt) {
				// "2" and a newline pasted together: typing, not a choice.
				m.typed = true
				m.selected = 0
				break
			}
			m.send(m.collect())
		case "esc", "q", "ctrl+c":
			m.send(AskResponseMsg{Cancelled: true})
		}
	}
	return m, nil
}

// isTyping reports whether k is text rather than one of the modal's keys:
// a paste, any printable key that is not bound, and every printable key
// once typing has started (so "just do it" does not move with its j).
func (m AskPromptModel) isTyping(k tea.KeyMsg) bool {
	if k.Paste {
		return true
	}
	if k.Type == tea.KeySpace {
		return m.typed || !m.multiSelect
	}
	if k.Type != tea.KeyRunes {
		return false
	}
	if m.typed || len(k.Runes) != 1 {
		return true
	}
	switch r := k.Runes[0]; {
	case r == 'j' || r == 'k' || r == 'q':
		return false
	case r >= '1' && r <= '9':
		return false
	}
	return true
}

// collect gathers the chosen labels (checked set for multi, cursor for single).
func (m AskPromptModel) collect() AskResponseMsg {
	var out []string
	if m.multiSelect {
		for i, opt := range m.options {
			if m.checked[i] {
				out = append(out, opt.Label)
			}
		}
	} else {
		out = []string{m.options[m.selected].Label}
	}
	return AskResponseMsg{Selected: out}
}

func (m *AskPromptModel) send(resp AskResponseMsg) {
	if m.response != nil {
		m.response <- resp
	}
	m.active = false
	m.response = nil
}

// Dismiss answers cancelled and closes the modal: its run has ended. The
// send never blocks (the bridge's channel is buffered; nobody may read it now).
func (m AskPromptModel) Dismiss() AskPromptModel {
	if m.response != nil {
		select {
		case m.response <- AskResponseMsg{Cancelled: true}:
		default:
		}
	}
	m.active = false
	m.response = nil
	return m
}

// block renders s at the modal's width (wrapping long lines).
func (m AskPromptModel) block(s string) string {
	return StatusBarStyle.Width(m.width).Render(s)
}

// questionLines is the question as rendered, one entry per row.
func (m AskPromptModel) questionLines() []string {
	title := lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true).Render("? " + m.question)
	return strings.Split(m.block(title), "\n")
}

// optionsView renders the options (cursor, checkbox, description).
func (m AskPromptModel) optionsView() string {
	var b strings.Builder
	for i, opt := range m.options {
		cursor := "  "
		if i == m.selected {
			cursor = "› "
		}
		box := ""
		if m.multiSelect {
			if m.checked[i] {
				box = "[x] "
			} else {
				box = "[ ] "
			}
		}
		line := cursor + box + opt.Label
		style := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.selected {
			style = lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true)
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(style.Render(line))
		if opt.Description != "" {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorTextMuted).Render("  " + opt.Description))
		}
	}
	return m.block(b.String())
}

// footer is the key help; scroll adds the PgUp/PgDn hint and position.
func (m AskPromptModel) footer(scroll string) string {
	f := "↑/↓ move • Enter select • Esc cancel"
	if m.multiSelect {
		f = "↑/↓ move • Space toggle • Enter confirm • Esc cancel"
	}
	if scroll != "" {
		f += " • PgUp/PgDn scroll " + scroll
	}
	if !m.deadline.IsZero() {
		f += " • expires at " + m.deadline.Format("15:04")
	}
	if m.typed {
		return m.block(lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render(askTypedHint))
	}
	return m.block(lipgloss.NewStyle().Foreground(ColorTextMuted).Render(f))
}

// questionRows is how many question rows fit (all of them with no cap).
func (m AskPromptModel) questionRows() int {
	n := len(m.questionLines())
	if m.height <= 0 {
		return n
	}
	fixed := lipgloss.Height(m.optionsView()) + lipgloss.Height(m.footer(fmt.Sprintf("(%d-%d of %d)", n, n, n)))
	rows := max(m.height-fixed, 1)
	return min(rows, n)
}

// maxOffset is the furthest the question can scroll.
func (m AskPromptModel) maxOffset() int {
	if !m.active {
		return 0
	}
	return len(m.questionLines()) - m.questionRows()
}

// scrollStep is one PgUp/PgDn: a window less one row of overlap.
func (m AskPromptModel) scrollStep() int {
	return max(m.questionRows()-1, 1)
}

func (m AskPromptModel) View() string {
	if !m.active {
		return ""
	}
	lines := m.questionLines()
	rows := m.questionRows()
	scroll := ""
	if rows < len(lines) {
		off := min(max(m.offset, 0), len(lines)-rows)
		lines = lines[off : off+rows]
		scroll = fmt.Sprintf("(%d-%d of %d)", off+1, off+rows, len(m.questionLines()))
	}
	return strings.Join(lines, "\n") + "\n" + m.optionsView() + "\n" + m.footer(scroll)
}
