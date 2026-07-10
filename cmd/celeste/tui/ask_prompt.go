package tui

import (
	"strings"

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
}

// NewAskPromptModel creates an inactive ask prompt.
func NewAskPromptModel() AskPromptModel {
	return AskPromptModel{checked: map[int]bool{}}
}

// Active reports whether a question is awaiting an answer.
func (m AskPromptModel) Active() bool { return m.active }

// SetSize sets the render width.
func (m *AskPromptModel) SetSize(w, _ int) { m.width = w }

func (m AskPromptModel) Update(msg tea.Msg) (AskPromptModel, tea.Cmd) {
	switch msg := msg.(type) {
	case AskRequestMsg:
		m.active = true
		m.question = msg.Question
		m.options = msg.Options
		m.multiSelect = msg.MultiSelect
		m.response = msg.Response
		m.selected = 0
		m.checked = map[int]bool{}

	case tea.KeyMsg:
		if !m.active {
			break
		}
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.options)-1 {
				m.selected++
			}
		case " ":
			if m.multiSelect {
				m.checked[m.selected] = !m.checked[m.selected]
			}
		case "enter":
			m.send(m.collect())
		case "esc", "q":
			m.send(AskResponseMsg{Cancelled: true})
		}
	}
	return m, nil
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

func (m AskPromptModel) View() string {
	if !m.active {
		return ""
	}
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true).Render("? " + m.question)
	b.WriteString(title + "\n")
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
		b.WriteString(style.Render(line))
		if opt.Description != "" {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorTextMuted).Render("  " + opt.Description))
		}
		b.WriteString("\n")
	}
	footer := "↑/↓ move • Enter select • Esc cancel"
	if m.multiSelect {
		footer = "↑/↓ move • Space toggle • Enter confirm • Esc cancel"
	}
	b.WriteString(lipgloss.NewStyle().Foreground(ColorTextMuted).Render(footer))
	return StatusBarStyle.Width(m.width).Render(b.String())
}
