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

// brailleSpinner is the braille spinner cycle for regular tools.
var brailleSpinner = []rune{'⣾', '⣽', '⣻', '⢿', '⡿', '⣟', '⣯', '⣷'}

// corruptionGlyphs are substituted for 1 frame during the glitch effect
// on element-named subagents — corruption-theme aesthetic.
var corruptionGlyphs = []string{"◈", "◆", "⬡", "⬢", "▣", "◉", "⊕", "⊗", "⌬", "☍", "✦", "⟐"}

// elementColors maps element names to their ANSI color codes.
var elementColors = map[string]string{
	"earth": "#10b981", // green
	"fire":  "#f59e0b", // amber
	"water": "#0891b2", // cyan
	"light": "#f0ecf8", // white
	"dark":  "#8b5cf6", // purple
	"wind":  "#d94f90", // pink
}

// toolProgressEntry tracks the state of a single tool execution.
type toolProgressEntry struct {
	callID      string
	name        string
	displayName string // optional override for element-named subagents
	element     string // element type (earth/fire/water/etc) for color + animation
	state       string // executing, done, failed, aborted
	message     string
	subMessage  string // nested status (subagent turn/tool)
	startedAt   time.Time
	doneAt      time.Time
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
			if msg.DisplayName != "" {
				m.entries[i].displayName = msg.DisplayName
			}
			if msg.Element != "" {
				m.entries[i].element = msg.Element
			}
			if msg.SubMessage != "" {
				m.entries[i].subMessage = msg.SubMessage
			}
			if msg.State == "executing" && e.state != "executing" {
				m.entries[i].startedAt = time.Now()
			} else if msg.State != "executing" {
				m.entries[i].doneAt = time.Now()
			}
			return
		}
	}
	// New entry
	entry := toolProgressEntry{
		callID:      msg.ToolCallID,
		name:        msg.ToolName,
		displayName: msg.DisplayName,
		element:     msg.Element,
		state:       msg.State,
		message:     msg.Message,
		subMessage:  msg.SubMessage,
		startedAt:   time.Now(),
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
	// Determine the element color (if this is a named subagent)
	elemColor := ""
	if e.element != "" {
		elemColor = elementColors[e.element]
	}

	var icon string
	var stateStyle lipgloss.Style

	switch e.state {
	case "executing":
		if e.element != "" && elemColor != "" {
			// Element-named subagent: kanji pulse + corruption glitch
			icon = m.elementSpinnerIcon(e.displayName, e.element)
			stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(elemColor))
		} else {
			// Regular tool: standard braille spinner
			icon = string(brailleSpinner[m.spinFrame])
			stateStyle = lipgloss.NewStyle().Foreground(ColorWarning)
		}
	case "done":
		icon = "✓"
		if elemColor != "" {
			stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(elemColor))
		} else {
			stateStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
		}
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

	var elapsed time.Duration
	if e.state == "executing" {
		elapsed = time.Since(e.startedAt)
	} else if !e.doneAt.IsZero() {
		elapsed = e.doneAt.Sub(e.startedAt)
	}
	elapsedStr := fmt.Sprintf("%.1fs", elapsed.Seconds())

	displayName := e.name
	if e.displayName != "" {
		displayName = e.displayName
	}

	// Name style — element color or default purple
	nameStyle := lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	if elemColor != "" {
		nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(elemColor)).Bold(true)
	}

	line := fmt.Sprintf(" %s %s %s %s",
		stateStyle.Render(icon),
		nameStyle.Render(displayName),
		stateStyle.Render(e.state),
		lipgloss.NewStyle().Foreground(ColorTextMuted).Render(elapsedStr),
	)

	// Nested subagent activity with element-colored prefix
	if e.subMessage != "" && e.state == "executing" {
		subColor := lipgloss.Color("#6d28d9")
		if elemColor != "" {
			subColor = lipgloss.Color(elemColor)
		}
		subStyle := lipgloss.NewStyle().Foreground(subColor)
		line += "\n   " + subStyle.Render("└─ "+e.subMessage)
	}

	return line
}

// elementSpinnerIcon returns the animated icon for element-named subagents.
// Three effects layered:
//   1. Element kanji as the base character (instead of braille)
//   2. Pulse: alternates bold/dim every other frame
//   3. Corruption glitch: ~1 in 8 frames replaces kanji with a corruption glyph
func (m ToolProgressModel) elementSpinnerIcon(displayName, element string) string {
	// Extract the kanji from displayName (first rune of "〔火 hi〕" → "火")
	kanji := "◈" // fallback
	for _, r := range displayName {
		if r != '〔' && r != ' ' && r != '〕' {
			kanji = string(r)
			break
		}
	}

	// Corruption glitch: 1 in 8 frames, replace with a random corruption glyph
	if m.spinFrame%8 == 3 {
		glyphIdx := (m.spinFrame / 8) % len(corruptionGlyphs)
		return corruptionGlyphs[glyphIdx]
	}

	// Kanji pulse: bold on even frames, dim (no bold) on odd
	if m.spinFrame%2 == 0 {
		return kanji
	}
	// Return the kanji but it'll be rendered dim by the caller's stateStyle
	// which already has the element color — the bold/non-bold alternation
	// creates the pulse effect.
	return kanji
}
