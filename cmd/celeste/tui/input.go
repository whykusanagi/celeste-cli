// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the input component with command history.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// knownCommands is the authoritative list of slash commands for typeahead.
var knownCommands = []string{
	"agent", "clear", "collections", "config", "context", "costs", "diff",
	"effort", "endpoint", "export", "graph", "grimoire", "help", "index", "mcp",
	"memories", "menu", "model", "nsfw", "orch", "orchestrate", "persona",
	"plan", "providers", "safe", "session", "set-model", "skills",
	"stats", "tools", "undo",
}

var (
	suggestionTabStyle    = lipgloss.NewStyle().Foreground(ColorTextMuted)
	suggestionActiveStyle = lipgloss.NewStyle().Foreground(ColorPurpleNeon).Bold(true)
	suggestionDimStyle    = lipgloss.NewStyle().Foreground(ColorTextMuted)
)

// computeSuggestions returns commands that start with the partial, excluding exact match.
func computeSuggestions(value string) []string {
	if !strings.HasPrefix(value, "/") {
		return nil
	}
	partial := strings.TrimPrefix(value, "/")
	partial = strings.TrimSpace(partial)
	if partial == "" {
		return nil
	}

	var matches []string
	for _, cmd := range knownCommands {
		if strings.HasPrefix(cmd, partial) && cmd != partial {
			matches = append(matches, cmd)
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
	tempInput     string   // Stores current input when browsing history
	suggestions   []string // Current typeahead matches
	suggestionIdx int      // Which suggestion is highlighted (Tab cycles)
}

// NewInputModel creates a new input model using textarea for word-wrap support.
func NewInputModel() InputModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message or 'help'..."
	ta.Focus()
	ta.CharLimit = 4096
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
	}
}

// SetWidth sets the input width.
func (m InputModel) SetWidth(width int) InputModel {
	if width < 20 {
		width = 80
	}
	m.width = width
	m.textArea.SetWidth(width - 4) // Account for borders/padding
	return m
}

// Value returns the current input value.
func (m InputModel) Value() string {
	return m.textArea.Value()
}

// SetValue sets the input text.
func (m InputModel) SetValue(s string) InputModel {
	m.textArea.SetValue(s)
	return m
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
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			// Complete with the highlighted suggestion
			if len(m.suggestions) > 0 {
				m.textArea.SetValue("/" + m.suggestions[m.suggestionIdx] + " ")
				m.suggestions = nil
				m.suggestionIdx = 0
			}
			return m, nil

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
				}
				return m, nil
			}

		case "ctrl+u":
			// Clear input line
			m.textArea.Reset()
			m.suggestions = nil
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
			return m, nil
		}
	}

	// Delegate to textarea for character input, cursor movement, etc.
	m.textArea, cmd = m.textArea.Update(msg)

	// Update suggestions based on current value
	m.suggestions = computeSuggestions(m.textArea.Value())
	if len(m.suggestions) > 0 && m.suggestionIdx >= len(m.suggestions) {
		m.suggestionIdx = 0
	}

	return m, cmd
}

// View renders the input component.
func (m InputModel) View() string {
	inputView := m.textArea.View()

	// Render typeahead suggestions below input
	var hintLine string
	if len(m.suggestions) > 0 {
		parts := make([]string, len(m.suggestions))
		for i, s := range m.suggestions {
			if i == m.suggestionIdx {
				parts[i] = suggestionActiveStyle.Render("/" + s)
			} else {
				parts[i] = suggestionDimStyle.Render("/" + s)
			}
		}
		hintLine = suggestionTabStyle.Render("  ") + strings.Join(parts, suggestionDimStyle.Render(" · "))
	}

	if hintLine != "" {
		return inputView + "\n" + hintLine
	}
	return inputView
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
