// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the input component with command history.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// knownCommands is the authoritative list of slash commands for typeahead.
var knownCommands = []string{
	"agent", "clear", "config", "context", "endpoint", "export",
	"help", "model", "nsfw", "orch", "orchestrate", "providers",
	"safe", "session", "set-model", "skills", "stats", "tools",
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
	if partial == "" || strings.Contains(partial, " ") {
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

// InputModel represents the text input component.
type InputModel struct {
	textInput     textinput.Model
	width         int
	history       []string
	historyIndex  int
	tempInput     string   // Stores current input when browsing history
	suggestions   []string // Current typeahead matches
	suggestionIdx int      // Which suggestion is highlighted (Tab cycles)
}

// NewInputModel creates a new input model.
func NewInputModel() InputModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message or 'help'..."
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80
	ti.PromptStyle = InputPromptStyle
	ti.TextStyle = InputTextStyle
	ti.PlaceholderStyle = InputPlaceholderStyle
	ti.Prompt = "❯ "

	return InputModel{
		textInput:    ti,
		history:      []string{},
		historyIndex: -1,
	}
}

// SetWidth sets the input width.
func (m InputModel) SetWidth(width int) InputModel {
	m.width = width
	m.textInput.Width = width - 8 // Account for prompt and padding
	return m
}

// Init implements the init method for InputModel.
func (m InputModel) Init() tea.Cmd {
	return textinput.Blink
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
				m.textInput.SetValue("/" + m.suggestions[m.suggestionIdx] + " ")
				m.textInput.CursorEnd()
				m.suggestions = nil
				m.suggestionIdx = 0
			}
			return m, nil

		case "enter":
			value := m.textInput.Value()
			if value != "" {
				// Add to history
				m.history = append(m.history, value)
				m.historyIndex = len(m.history) // Reset index past end
				m.tempInput = ""
				m.suggestions = nil
				m.suggestionIdx = 0

				// Clear input
				m.textInput.Reset()

				// Send message
				return m, SendMessage(value)
			}

		case "up":
			// Browse history backwards
			if len(m.history) > 0 {
				if m.historyIndex == len(m.history) {
					// Save current input before browsing
					m.tempInput = m.textInput.Value()
				}
				if m.historyIndex > 0 {
					m.historyIndex--
					m.textInput.SetValue(m.history[m.historyIndex])
					m.textInput.CursorEnd()
				}
			}
			return m, nil

		case "down":
			// Browse history forwards
			if len(m.history) > 0 && m.historyIndex < len(m.history) {
				m.historyIndex++
				if m.historyIndex == len(m.history) {
					// Restore saved input
					m.textInput.SetValue(m.tempInput)
				} else {
					m.textInput.SetValue(m.history[m.historyIndex])
				}
				m.textInput.CursorEnd()
			}
			return m, nil

		case "ctrl+u":
			// Clear input line
			m.textInput.Reset()
			return m, nil

		case "ctrl+w":
			// Delete word backwards
			value := m.textInput.Value()
			if len(value) > 0 {
				// Find last space
				lastSpace := -1
				for i := len(value) - 2; i >= 0; i-- {
					if value[i] == ' ' {
						lastSpace = i
						break
					}
				}
				if lastSpace >= 0 {
					m.textInput.SetValue(value[:lastSpace+1])
				} else {
					m.textInput.Reset()
				}
				m.textInput.CursorEnd()
			}
			return m, nil
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)

	// Recompute typeahead after every keystroke
	m.suggestions = computeSuggestions(m.textInput.Value())
	if m.suggestionIdx >= len(m.suggestions) {
		m.suggestionIdx = 0
	}

	return m, cmd
}

// View renders the input component.
func (m InputModel) View() string {
	inputLine := m.textInput.View()

	// Always render a hint line to keep layout height stable at 3 lines.
	// When suggestions exist, show them; otherwise show a blank line.
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
		hintLine = suggestionTabStyle.Render("  ↹ ") + strings.Join(parts, suggestionTabStyle.Render("  "))
	}

	return InputPanelStyle.
		Width(m.width).
		Render(inputLine + "\n" + hintLine)
}

// Value returns the current input value.
func (m InputModel) Value() string {
	return m.textInput.Value()
}

// Focus focuses the input.
func (m InputModel) Focus() InputModel {
	m.textInput.Focus()
	return m
}

// Blur removes focus from the input.
func (m InputModel) Blur() InputModel {
	m.textInput.Blur()
	return m
}

// SetValue sets the input value.
func (m InputModel) SetValue(value string) InputModel {
	m.textInput.SetValue(value)
	return m
}

// Clear clears the input.
func (m InputModel) Clear() InputModel {
	m.textInput.Reset()
	return m
}

// GetHistory returns the command history.
func (m InputModel) GetHistory() []string {
	return m.history
}

// SetHistory sets the command history.
func (m InputModel) SetHistory(history []string) InputModel {
	m.history = history
	m.historyIndex = len(history)
	return m
}
