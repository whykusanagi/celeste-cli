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
}

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

	case tea.KeyMsg:
		if !m.active {
			break
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
		case "d":
			resp = PermissionResponse{Decision: "deny"}
		case "D":
			resp = PermissionResponse{
				Decision: "always_deny",
				Pattern:  m.buildPattern(),
			}
		default:
			// Ignore unrecognized keys
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

// buildPattern constructs a rule pattern from the current tool info.
func (m PermissionPromptModel) buildPattern() string {
	return fmt.Sprintf("%s(%s)", m.toolName, m.inputSummary)
}

// View renders the permission prompt dialog.
func (m PermissionPromptModel) View() string {
	if !m.active {
		return ""
	}

	w := m.width
	if w < 40 {
		w = 44
	}
	innerW := w - 6
	if innerW < 30 {
		innerW = 30
	}

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

	hRule := strings.Repeat("─", innerW)
	title := " Permission Required "

	// Top border
	topFill := innerW - len(title)
	if topFill < 0 {
		topFill = 0
	}
	top := borderStyle.Render("╭─") + titleStyle.Render(title) + borderStyle.Render(strings.Repeat("─", topFill)+"╮")

	// Content lines
	pad := func(s string) string {
		visible := len(s) // approximate; lipgloss styled strings are longer
		_ = visible
		return borderStyle.Render("│") + "  " + s + borderStyle.Render("")
	}

	// Summary line
	summaryLine := pad(textStyle.Render(fmt.Sprintf("%s wants to run: %s", m.toolName, m.inputSummary)))
	riskLine := pad(textStyle.Render("Risk: ") + riskStyle.Render(m.riskLevel))
	blankLine := borderStyle.Render("│") + strings.Repeat(" ", innerW+2) + borderStyle.Render("│")
	_ = blankLine

	pattern := m.buildPattern()
	optA := pad(keyStyle.Render("[a]") + mutedStyle.Render(" Allow once"))
	optAA := pad(keyStyle.Render("[A]") + mutedStyle.Render(fmt.Sprintf(" Always allow %q", pattern)))
	optD := pad(keyStyle.Render("[d]") + mutedStyle.Render(" Deny"))
	optDD := pad(keyStyle.Render("[D]") + mutedStyle.Render(fmt.Sprintf(" Always deny %q", pattern)))

	// Bottom border
	bot := borderStyle.Render("╰" + hRule + "──╯")

	lines := []string{
		top,
		summaryLine,
		riskLine,
		"",
		optA,
		optAA,
		optD,
		optDD,
		bot,
	}

	return strings.Join(lines, "\n")
}
