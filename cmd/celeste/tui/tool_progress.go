// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the tool progress component that tracks active tool executions.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// brailleSpinner is the braille spinner cycle for executing tools.
var brailleSpinner = []rune{'⣾', '⣽', '⣻', '⢿', '⡿', '⣟', '⣯', '⣷'}

// toolProgressEntry tracks the state of a single tool execution.
type toolProgressEntry struct {
	callID    string
	name      string
	state     string // executing, done, failed, aborted
	message   string
	elapsed   time.Duration
	startedAt time.Time
	doneAt    time.Time
}

// ToolProgressModel displays stacked tool execution cards.
type ToolProgressModel struct {
	entries   []toolProgressEntry
	width     int
	spinFrame int
}

// NewToolProgressModel creates a new tool progress model.
func NewToolProgressModel() ToolProgressModel {
	return ToolProgressModel{}
}

// Init initializes the model (no-op).
func (m ToolProgressModel) Init() tea.Cmd {
	return nil
}

// SetSize updates the component width.
func (m *ToolProgressModel) SetSize(width, _ int) {
	m.width = width
}

// Update handles messages for the tool progress component.
func (m ToolProgressModel) Update(msg tea.Msg) (ToolProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ToolProgressMsg:
		m.handleProgress(msg)

	case TickMsg:
		// Advance spinner frame
		m.spinFrame = (m.spinFrame + 1) % len(brailleSpinner)
		// Prune completed entries older than 2 seconds
		m.pruneOldEntries()
	}
	return m, nil
}

// handleProgress updates or adds a tool progress entry.
func (m *ToolProgressModel) handleProgress(msg ToolProgressMsg) {
	for i, e := range m.entries {
		if e.callID == msg.ToolCallID {
			m.entries[i].state = msg.State
			m.entries[i].message = msg.Message
			m.entries[i].elapsed = msg.Elapsed
			if msg.State != "executing" {
				m.entries[i].doneAt = time.Now()
			}
			return
		}
	}
	// New entry
	entry := toolProgressEntry{
		callID:    msg.ToolCallID,
		name:      msg.ToolName,
		state:     msg.State,
		message:   msg.Message,
		elapsed:   msg.Elapsed,
		startedAt: time.Now(),
	}
	if msg.State != "executing" {
		entry.doneAt = time.Now()
	}
	m.entries = append(m.entries, entry)
}

// pruneOldEntries removes completed entries older than 2 seconds.
func (m *ToolProgressModel) pruneOldEntries() {
	now := time.Now()
	kept := m.entries[:0]
	for _, e := range m.entries {
		if e.state == "executing" || now.Sub(e.doneAt) < 2*time.Second {
			kept = append(kept, e)
		}
	}
	m.entries = kept
}

// HasActive returns true if there are any entries to display.
func (m ToolProgressModel) HasActive() bool {
	return len(m.entries) > 0
}

// View renders the tool progress cards.
func (m ToolProgressModel) View() string {
	if len(m.entries) == 0 {
		return ""
	}

	w := m.width
	if w < 30 {
		w = 40
	}
	// Reserve space for border
	innerW := w - 4
	if innerW < 20 {
		innerW = 20
	}

	var cards []string
	for _, e := range m.entries {
		cards = append(cards, m.renderEntry(e, innerW))
	}

	return strings.Join(cards, "\n")
}

// renderEntry renders a single tool progress card.
func (m ToolProgressModel) renderEntry(e toolProgressEntry, innerW int) string {
	// Status icon and label
	var icon string
	var stateStyle lipgloss.Style
	switch e.state {
	case "executing":
		icon = string(brailleSpinner[m.spinFrame])
		stateStyle = lipgloss.NewStyle().Foreground(ColorWarning)
	case "done":
		icon = "✓"
		stateStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
	case "failed":
		icon = "✗"
		stateStyle = lipgloss.NewStyle().Foreground(ColorError)
	case "aborted":
		icon = "✗"
		stateStyle = lipgloss.NewStyle().Foreground(ColorTextMuted)
	default:
		icon = "?"
		stateStyle = lipgloss.NewStyle().Foreground(ColorTextMuted)
	}

	elapsed := fmt.Sprintf("%.1fs", e.elapsed.Seconds())

	// Collapsed single-line for completed entries
	if e.state != "executing" && !e.doneAt.IsZero() && time.Since(e.doneAt) > 500*time.Millisecond {
		line := fmt.Sprintf(" %s %s %s %s",
			stateStyle.Render(icon),
			lipgloss.NewStyle().Foreground(ColorPurple).Bold(true).Render(e.name),
			stateStyle.Render(e.state),
			lipgloss.NewStyle().Foreground(ColorTextMuted).Render(elapsed),
		)
		return line
	}

	// Full card with border
	headerLabel := fmt.Sprintf(" %s ", e.name)
	statusLabel := fmt.Sprintf(" %s %s", icon, e.state)

	// Top line: ┌─ name ────── ⣾ executing
	topFill := innerW - len(headerLabel) - len(statusLabel) - 2
	if topFill < 1 {
		topFill = 1
	}
	topLine := fmt.Sprintf("┌─%s%s%s",
		lipgloss.NewStyle().Foreground(ColorPurple).Bold(true).Render(headerLabel),
		lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", topFill)),
		stateStyle.Render(statusLabel),
	)

	// Middle line: message (if any)
	var midLine string
	if e.message != "" {
		msg := e.message
		if len(msg) > innerW-4 {
			msg = msg[:innerW-7] + "..."
		}
		midLine = fmt.Sprintf("%s  %s",
			lipgloss.NewStyle().Foreground(ColorBorder).Render("│"),
			lipgloss.NewStyle().Foreground(ColorTextSecondary).Render(msg),
		)
	}

	// Bottom line: └──────── 1.2s
	elapsedLabel := " " + elapsed
	botFill := innerW - len(elapsedLabel) - 1
	if botFill < 1 {
		botFill = 1
	}
	botLine := fmt.Sprintf("%s%s",
		lipgloss.NewStyle().Foreground(ColorBorder).Render("└"+strings.Repeat("─", botFill)),
		lipgloss.NewStyle().Foreground(ColorTextMuted).Render(elapsedLabel),
	)

	if midLine != "" {
		return topLine + "\n" + midLine + "\n" + botLine
	}
	return topLine + "\n" + botLine
}
