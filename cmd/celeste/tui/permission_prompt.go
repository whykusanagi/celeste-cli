// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the permission prompt dialog for tool execution approval.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PermissionPromptModel renders an inline permission dialog.
type PermissionPromptModel struct {
	active       bool
	toolName     string
	inputSummary string
	riskLevel    string
	response     chan PermissionResponse
	width        int
	// typed is set by a printable key that is not one of the prompt's
	// (typing meant for the input, a paste). While it is set every
	// printable key is text, so the a in "/plan off" allows nothing, and
	// a hint says how to answer; Enter clears it.
	typed bool
}

// permissionTypedHint is shown while typed keys are being ignored.
const permissionTypedHint = "Typing is ignored here. Press a, A, d or D (Enter to dismiss this hint, Esc denies)"

// NewPermissionPromptModel creates a new permission prompt model.
func NewPermissionPromptModel() PermissionPromptModel {
	return PermissionPromptModel{}
}

// Init initializes the model (no-op).
func (m PermissionPromptModel) Init() tea.Cmd {
	return nil
}

// SetSize updates the component width.
func (m *PermissionPromptModel) SetSize(width, _ int) {
	m.width = width
}

// Active returns whether the permission prompt is currently displayed.
func (m PermissionPromptModel) Active() bool {
	return m.active
}

// Update handles messages for the permission prompt.
func (m PermissionPromptModel) Update(msg tea.Msg) (PermissionPromptModel, tea.Cmd) {
	switch msg := msg.(type) {
	case PermissionRequestMsg:
		m.active = true
		m.toolName = msg.ToolName
		m.inputSummary = msg.InputSummary
		m.riskLevel = msg.RiskLevel
		m.response = msg.Response
		m.typed = false

	case tea.KeyMsg:
		if !m.active {
			break
		}
		if msg.Paste || msg.Type == tea.KeySpace || (msg.Type == tea.KeyRunes && m.typed) {
			m.typed = true
			return m, nil
		}
		var resp PermissionResponse
		switch msg.String() {
		case "a":
			resp = PermissionResponse{Decision: "allow_once"}
		case "A":
			resp = PermissionResponse{
				Decision: "always_allow",
				Pattern:  m.buildPattern(),
			}
		case "d", "esc", "ctrl+c":
			// Esc and Ctrl+C dismiss the modal as a denial, so a waiting
			// run (an /orch lane, /agent) is never stuck on it.
			resp = PermissionResponse{Decision: "deny"}
		case "D":
			resp = PermissionResponse{
				Decision: "always_deny",
				Pattern:  m.buildPattern(),
			}
		case "enter":
			// There is no default answer: Enter only clears the hint.
			m.typed = false
			return m, nil
		default:
			// Any other printable key is typing; other keys do nothing.
			if msg.Type == tea.KeyRunes {
				m.typed = true
			}
			return m, nil
		}
		// Send response and deactivate
		if m.response != nil {
			m.response <- resp
		}
		m.active = false
		m.response = nil
	}
	return m, nil
}

// Dismiss answers deny and closes the modal: its run has ended. The send
// never blocks (the bridge's channel is buffered; nobody may read it now).
func (m PermissionPromptModel) Dismiss() PermissionPromptModel {
	if m.response != nil {
		select {
		case m.response <- PermissionResponse{Decision: "deny"}:
		default:
		}
	}
	m.active = false
	m.response = nil
	return m
}

// buildPattern constructs a rule pattern from the current tool info.
// It returns the bare tool name so that the persisted always-allow/deny rule
// matches ANY future invocation of the same tool, regardless of arguments.
// MatchRule treats a bare name as an exact tool-name match with no argument
// glob, which is the correct "always allow/deny this tool" semantic.
// The truncated inputSummary is used for display only (modal text), not here.
func (m PermissionPromptModel) buildPattern() string {
	return m.toolName
}

// View renders the permission prompt dialog: a closed box exactly as wide
// as the terminal (44 columns at least), long lines wrapped inside it.
func (m PermissionPromptModel) View() string {
	if !m.active {
		return ""
	}

	w := max(m.width, 44)
	inner := w - 2     // between the side borders
	textW := inner - 4 // two columns of padding on each side

	borderStyle := lipgloss.NewStyle().Foreground(ColorBorderGlow)
	titleStyle := lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(ColorText)
	keyStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)

	// Risk level color
	var riskStyle lipgloss.Style
	switch m.riskLevel {
	case "destructive":
		riskStyle = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	case "write":
		riskStyle = lipgloss.NewStyle().Foreground(ColorWarning)
	default:
		riskStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
	}

	side := borderStyle.Render("│")
	var lines []string
	// row adds s, wrapped to the box, one bordered row per line.
	row := func(s string) {
		for _, l := range strings.Split(lipgloss.NewStyle().Width(textW).Render(s), "\n") {
			fill := max(textW-lipgloss.Width(l), 0)
			lines = append(lines, side+"  "+l+strings.Repeat(" ", fill)+"  "+side)
		}
	}

	title := " Permission Required "
	topFill := max(inner-1-lipgloss.Width(title), 0)
	lines = append(lines, borderStyle.Render("╭─")+titleStyle.Render(title)+borderStyle.Render(strings.Repeat("─", topFill)+"╮"))

	row(textStyle.Render(fmt.Sprintf("%s wants to run: %s", m.toolName, m.inputSummary)))
	row(textStyle.Render("Risk: ") + riskStyle.Render(m.riskLevel))
	lines = append(lines, side+strings.Repeat(" ", inner)+side)

	pattern := m.buildPattern()
	row(keyStyle.Render("[a]") + mutedStyle.Render(" Allow once"))
	row(keyStyle.Render("[A]") + mutedStyle.Render(fmt.Sprintf(" Always allow %q", pattern)))
	row(keyStyle.Render("[d]") + mutedStyle.Render(" Deny (also Esc)"))
	row(keyStyle.Render("[D]") + mutedStyle.Render(fmt.Sprintf(" Always deny %q", pattern)))
	if m.typed {
		row(lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render(permissionTypedHint))
	}

	lines = append(lines, borderStyle.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(lines, "\n")
}
