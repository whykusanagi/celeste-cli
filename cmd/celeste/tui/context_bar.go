// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the context budget bar that displays token usage.
package tui

import (
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
)

// ContextBarModel renders a thin status bar showing token budget usage.
type ContextBarModel struct {
	usedTokens   int
	maxTokens    int
	usagePercent float64
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
		m.turnCount = msg.TurnCount
	}
	return m, nil
}

// resetUsage zeroes the usage and turn count for a new session, keeping the
// window size so the bar still shows.
func (m ContextBarModel) resetUsage() ContextBarModel {
	m.usedTokens, m.usagePercent, m.turnCount = 0, 0, 0
	return m
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
	// A negative count from a saved session, or a NaN, shows as 0%; an
	// over-budget usage keeps its real figure in the label.
	label := m.usagePercent
	if !(label > 0) { // also catches NaN
		label = 0
	}
	if math.IsInf(label, 1) {
		label = 100
	}
	pctStr := fmt.Sprintf("%.0f%%", label)

	// Progress bar: 10 segments, clamped before converting so strings.Repeat
	// never sees a negative count.
	filled := int(min(label, 100) / 10)
	empty := 10 - filled
	bar := barStyle.Render(strings.Repeat("▓", filled)) + emptyStyle.Render(strings.Repeat("░", empty))

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
	return fmt.Sprintf(" %s %s %s / %s %s %s  %s  %s",
		diamondStyle.Render("◆"),
		labelStyle.Render("tokens:"),
		labelStyle.Render(usedStr),
		labelStyle.Render(maxStr),
		bar,
		labelStyle.Render(pctStr),
		labelStyle.Render("│"),
		labelStyle.Render(fmt.Sprintf("turn: %d", m.turnCount)),
	)
}
