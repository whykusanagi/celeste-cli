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
	startedAt time.Time
	doneAt    time.Time
}

// ToolProgressModel displays stacked tool execution cards.
// Entries auto-clear when the user sends a new message.
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

// ClearCompleted removes all non-executing entries.
// Called when the user sends a new message so old results don't accumulate.
func (m *ToolProgressModel) ClearCompleted() {
	kept := m.entries[:0]
	for _, e := range m.entries {
		if e.state == "executing" {
			kept = append(kept, e)
		}
	}
	m.entries = kept
}

// Update handles messages for the tool progress component.
func (m ToolProgressModel) Update(msg tea.Msg) (ToolProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ToolProgressMsg:
		m.handleProgress(msg)
	case TickMsg:
		m.spinFrame = (m.spinFrame + 1) % len(brailleSpinner)
	}
	return m, nil
}

// handleProgress updates or adds a tool progress entry.
func (m *ToolProgressModel) handleProgress(msg ToolProgressMsg) {
	for i, e := range m.entries {
		if e.callID == msg.ToolCallID {
			m.entries[i].state = msg.State
			m.entries[i].message = msg.Message
			if msg.State == "executing" && e.state != "executing" {
				// Tool is starting now (was queued) — reset start time
				m.entries[i].startedAt = time.Now()
			} else if msg.State != "executing" {
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
		startedAt: time.Now(),
	}
	if msg.State != "executing" {
		entry.doneAt = time.Now()
	}
	m.entries = append(m.entries, entry)
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

	var lines []string
	for _, e := range m.entries {
		lines = append(lines, m.renderEntry(e))
	}

	return strings.Join(lines, "\n")
}

// renderEntry renders a single tool progress line.
func (m ToolProgressModel) renderEntry(e toolProgressEntry) string {
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

	// Compute elapsed from startedAt
	var elapsed time.Duration
	if e.state == "executing" {
		elapsed = time.Since(e.startedAt)
	} else if !e.doneAt.IsZero() {
		elapsed = e.doneAt.Sub(e.startedAt)
	}

	elapsedStr := fmt.Sprintf("%.1fs", elapsed.Seconds())

	return fmt.Sprintf(" %s %s %s %s",
		stateStyle.Render(icon),
		lipgloss.NewStyle().Foreground(ColorPurple).Bold(true).Render(e.name),
		stateStyle.Render(e.state),
		lipgloss.NewStyle().Foreground(ColorTextMuted).Render(elapsedStr),
	)
}
