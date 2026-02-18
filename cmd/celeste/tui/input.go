// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the input component with command history.
package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputModel represents the text input component.
type InputModel struct {
	textInput    textinput.Model
	width        int
	history      []string
	historyIndex int
	tempInput    string // Stores current input when browsing history
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
		case "enter":
			value := m.textInput.Value()
			if value != "" {
				// Add to history
				m.history = append(m.history, value)
				m.historyIndex = len(m.history) // Reset index past end
				m.tempInput = ""

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
	return m, cmd
}

// View renders the input component.
func (m InputModel) View() string {
	// Create the input line - compact, no extra lines
	input := m.textInput.View()

	return InputPanelStyle.
		Width(m.width).
		Render(input)
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
