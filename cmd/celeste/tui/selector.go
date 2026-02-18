// Package tui provides the interactive selector component.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SelectorItem represents an item in the selector.
type SelectorItem struct {
	ID          string // Model ID or config name
	DisplayName string // Human-readable name
	Description string // Additional info
	Badge       string // Optional badge (✓ for tool support, etc.)
}

// SelectorModel is a Bubble Tea component for arrow-key navigation.
type SelectorModel struct {
	title      string
	items      []SelectorItem
	selected   int    // Currently selected index
	offset     int    // Scroll offset for large lists
	height     int    // Visible height
	width      int    // Component width
	active     bool   // Whether selector is active
	confirmed  bool   // Whether user pressed Enter
	cancelled  bool   // Whether user pressed Escape
	footerText string // Footer instructions
}

// NewSelectorModel creates a new selector component.
func NewSelectorModel(title string, items []SelectorItem) SelectorModel {
	return SelectorModel{
		title:      title,
		items:      items,
		selected:   0,
		offset:     0,
		height:     10, // Default visible items
		width:      80,
		active:     true,
		footerText: "↑/↓: Navigate • Enter: Select • Esc: Cancel",
	}
}

// SelectorResultMsg is sent when the selector completes.
type SelectorResultMsg struct {
	Selected  *SelectorItem // nil if cancelled
	Cancelled bool
}

// Init implements tea.Model.
func (m SelectorModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m SelectorModel) Update(msg tea.Msg) (SelectorModel, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				// Scroll up if needed
				if m.selected < m.offset {
					m.offset = m.selected
				}
			}

		case "down", "j":
			if m.selected < len(m.items)-1 {
				m.selected++
				// Scroll down if needed
				if m.selected >= m.offset+m.height {
					m.offset = m.selected - m.height + 1
				}
			}

		case "pgup":
			// Jump up by page
			m.selected -= m.height
			if m.selected < 0 {
				m.selected = 0
			}
			m.offset = m.selected

		case "pgdown":
			// Jump down by page
			m.selected += m.height
			if m.selected >= len(m.items) {
				m.selected = len(m.items) - 1
			}
			// Adjust offset
			if m.selected >= m.offset+m.height {
				m.offset = m.selected - m.height + 1
			}

		case "home", "g":
			// Jump to top
			m.selected = 0
			m.offset = 0

		case "end", "G":
			// Jump to bottom
			m.selected = len(m.items) - 1
			m.offset = m.selected - m.height + 1
			if m.offset < 0 {
				m.offset = 0
			}

		case "enter":
			// Confirm selection
			m.confirmed = true
			m.active = false
			if m.selected >= 0 && m.selected < len(m.items) {
				return m, func() tea.Msg {
					return SelectorResultMsg{
						Selected:  &m.items[m.selected],
						Cancelled: false,
					}
				}
			}

		case "esc", "q":
			// Cancel selection
			m.cancelled = true
			m.active = false
			return m, func() tea.Msg {
				return SelectorResultMsg{
					Selected:  nil,
					Cancelled: true,
				}
			}
		}
	}

	return m, nil
}

// View implements tea.Model.
func (m SelectorModel) View() string {
	if !m.active && !m.confirmed && !m.cancelled {
		return ""
	}

	var b strings.Builder

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorAccentGlow).
		Background(lipgloss.Color("#3a1f3a")).
		Bold(true)

	itemStyle := lipgloss.NewStyle().
		Foreground(ColorPurpleNeon)

	badgeStyle := lipgloss.NewStyle().
		Foreground(ColorCyan)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Faint(true)

	footerStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Italic(true).
		MarginTop(1)

	// Title
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")

	// Separator
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n\n")

	// Calculate visible range
	visibleEnd := m.offset + m.height
	if visibleEnd > len(m.items) {
		visibleEnd = len(m.items)
	}

	// Show scroll indicator if needed
	if m.offset > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorCyan).Render("    ▲ More above\n"))
	}

	// Render visible items
	for i := m.offset; i < visibleEnd; i++ {
		item := m.items[i]
		cursor := "  "
		if i == m.selected {
			cursor = "▶ "
		}

		// Format: [cursor] [name] [badge] [description]
		line := fmt.Sprintf("%s%s", cursor, item.DisplayName)
		if item.Badge != "" {
			line += " " + badgeStyle.Render(item.Badge)
		}
		if item.Description != "" {
			line += " " + descStyle.Render("- "+item.Description)
		}

		if i == m.selected {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(itemStyle.Render(line))
		}
		b.WriteString("\n")
	}

	// Show scroll indicator if needed
	if visibleEnd < len(m.items) {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorCyan).Render("    ▼ More below\n"))
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render(m.footerText))

	// Status indicator
	countText := fmt.Sprintf("\n\nItem %d of %d", m.selected+1, len(m.items))
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render(countText))

	return b.String()
}

// SetHeight sets the visible height of the selector.
func (m SelectorModel) SetHeight(height int) SelectorModel {
	m.height = height
	return m
}

// SetWidth sets the width of the selector.
func (m SelectorModel) SetWidth(width int) SelectorModel {
	m.width = width
	return m
}

// IsActive returns whether the selector is currently active.
func (m SelectorModel) IsActive() bool {
	return m.active
}

// GetSelected returns the currently selected item (nil if none).
func (m SelectorModel) GetSelected() *SelectorItem {
	if m.selected >= 0 && m.selected < len(m.items) {
		return &m.items[m.selected]
	}
	return nil
}
