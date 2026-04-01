// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the context budget bar that displays token usage.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
)

// ContextBarModel renders a thin status bar showing token budget usage.
type ContextBarModel struct {
	usedTokens   int
	maxTokens    int
	usagePercent float64
	compactCount int
	turnCount    int
	width        int
}

// NewContextBarModel creates a new context bar model.
func NewContextBarModel() ContextBarModel {
	return ContextBarModel{}
}

// Init initializes the model (no-op).
func (m ContextBarModel) Init() tea.Cmd {
	return nil
}

// SetSize updates the component width.
func (m *ContextBarModel) SetSize(width, _ int) {
	m.width = width
}

// Update handles messages for the context bar.
func (m ContextBarModel) Update(msg tea.Msg) (ContextBarModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ContextBudgetMsg:
		m.usedTokens = msg.UsedTokens
		m.maxTokens = msg.MaxTokens
		m.usagePercent = msg.UsagePercent
		m.compactCount = msg.CompactCount
		m.turnCount = msg.TurnCount
	}
	return m, nil
}

// View renders the context budget bar.
func (m ContextBarModel) View() string {
	if m.maxTokens == 0 {
		return ""
	}

	// Choose progress bar color based on usage
	var barColor lipgloss.Color
	switch {
	case m.usagePercent > 80:
		barColor = lipgloss.Color(ColorError)
	case m.usagePercent > 50:
		barColor = lipgloss.Color(ColorWarning)
	default:
		barColor = lipgloss.Color(ColorSuccess)
	}

	barStyle := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)
	diamondStyle := lipgloss.NewStyle().Foreground(ColorPurple)

	usedStr := ctxmgr.FormatTokenCount(m.usedTokens)
	maxStr := ctxmgr.FormatTokenCount(m.maxTokens)
	pctStr := fmt.Sprintf("%.0f%%", m.usagePercent)

	// Progress bar: 10 segments
	filled := int(m.usagePercent / 10)
	if filled > 10 {
		filled = 10
	}
	empty := 10 - filled
	bar := barStyle.Render(repeatStr("▓", filled)) + emptyStyle.Render(repeatStr("░", empty))

	// Narrow terminal: minimal display
	if m.width > 0 && m.width < 80 {
		return fmt.Sprintf(" %s %s/%s %s %s",
			diamondStyle.Render("◆"),
			labelStyle.Render(usedStr),
			labelStyle.Render(maxStr),
			bar,
			labelStyle.Render(pctStr),
		)
	}

	// Full display
	return fmt.Sprintf(" %s %s %s / %s %s %s  %s  %s  %s  %s",
		diamondStyle.Render("◆"),
		labelStyle.Render("tokens:"),
		labelStyle.Render(usedStr),
		labelStyle.Render(maxStr),
		bar,
		labelStyle.Render(pctStr),
		labelStyle.Render("│"),
		labelStyle.Render(fmt.Sprintf("compact: %d", m.compactCount)),
		labelStyle.Render("│"),
		labelStyle.Render(fmt.Sprintf("turn: %d", m.turnCount)),
	)
}

// repeatStr repeats a string n times.
func repeatStr(s string, n int) string {
	result := ""
	for range n {
		result += s
	}
	return result
}
