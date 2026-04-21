// Session picker panel — browsable list of saved sessions.
// Replaces the text-dump /session list with an interactive UI.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// SessionEntry is a display-ready session summary.
type SessionEntry struct {
	ID           string
	Preview      string // first user message, truncated
	MessageCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SessionPanelModel is the interactive session picker.
type SessionPanelModel struct {
	entries  []SessionEntry
	cursor   int
	width    int
	height   int
	selected string // ID of selected session (empty = none)
	deleted  string // ID of deleted session
	err      error
}

// NewSessionPanelModel loads sessions and creates the picker.
func NewSessionPanelModel() SessionPanelModel {
	m := SessionPanelModel{}
	m.loadSessions()
	return m
}

func (m *SessionPanelModel) loadSessions() {
	mgr := config.NewSessionManager()
	sessions, err := mgr.List()
	if err != nil {
		m.err = err
		return
	}

	m.entries = make([]SessionEntry, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]

		// Skip empty sessions
		if len(s.Messages) == 0 {
			continue
		}

		entry := SessionEntry{
			ID:           s.ID,
			MessageCount: len(s.Messages),
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
		}

		// Extract first user message as preview
		for _, msg := range s.Messages {
			if msg.Role == "user" && strings.TrimSpace(msg.Content) != "" {
				preview := strings.ReplaceAll(msg.Content, "\n", " ")
				if len(preview) > 80 {
					preview = preview[:77] + "..."
				}
				entry.Preview = preview
				break
			}
		}
		if entry.Preview == "" {
			entry.Preview = "(no user messages)"
		}

		m.entries = append(m.entries, entry)
	}

	// Sort by updated time, most recent first
	for i := 0; i < len(m.entries); i++ {
		for j := i + 1; j < len(m.entries); j++ {
			if m.entries[j].UpdatedAt.After(m.entries[i].UpdatedAt) {
				m.entries[i], m.entries[j] = m.entries[j], m.entries[i]
			}
		}
	}
}

// SetWidth updates the panel width.
func (m SessionPanelModel) SetWidth(w int) SessionPanelModel {
	m.width = w
	return m
}

// SetHeight updates the panel height.
func (m SessionPanelModel) SetHeight(h int) SessionPanelModel {
	m.height = h
	return m
}

// Selected returns the ID of the session the user chose (empty if none).
func (m SessionPanelModel) Selected() string { return m.selected }

// Deleted returns the ID of a session the user deleted (empty if none).
func (m SessionPanelModel) Deleted() string { return m.deleted }

// Update handles input for the session picker.
func (m SessionPanelModel) Update(msg tea.Msg) (SessionPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "pgup":
			m.cursor -= 10
			if m.cursor < 0 {
				m.cursor = 0
			}
		case "pgdown":
			m.cursor += 10
			if m.cursor >= len(m.entries) {
				m.cursor = len(m.entries) - 1
			}
		case "enter":
			if len(m.entries) > 0 {
				m.selected = m.entries[m.cursor].ID
			}
		case "d", "delete", "backspace":
			if len(m.entries) > 0 {
				m.deleted = m.entries[m.cursor].ID
				// Remove from display
				m.entries = append(m.entries[:m.cursor], m.entries[m.cursor+1:]...)
				if m.cursor >= len(m.entries) && m.cursor > 0 {
					m.cursor--
				}
			}
		}
	}
	return m, nil
}

// View renders the session picker.
func (m SessionPanelModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error loading sessions: %v", m.err)
	}

	if len(m.entries) == 0 {
		return "No saved sessions.\n\nPress q or Esc to close."
	}

	w := m.width - 4
	if w < 40 {
		w = 40
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(ColorPurple).
		Bold(true)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6b7280"))

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Sessions (%d)", len(m.entries))))
	sb.WriteString("\n")
	sb.WriteString(hintStyle.Render("↑/↓ navigate  PgUp/PgDn page  Enter resume  d delete  Esc close"))
	sb.WriteString("\n\n")

	// Visible window — show a page of entries around the cursor
	pageSize := 10
	if m.height > 30 {
		pageSize = (m.height - 8) / 3 // 3 lines per entry (meta + preview + gap)
	}

	start := 0
	if m.cursor >= pageSize {
		start = m.cursor - pageSize + 1
	}
	end := start + pageSize
	if end > len(m.entries) {
		end = len(m.entries)
	}

	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorPurpleNeon).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	previewStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#d1d5db"))

	for i := start; i < end; i++ {
		e := m.entries[i]
		cursor := "  "
		style := dimStyle
		pStyle := previewStyle

		if i == m.cursor {
			cursor = "▶ "
			style = selectedStyle
			pStyle = selectedStyle
		}

		age := formatAge(e.UpdatedAt)
		meta := style.Render(fmt.Sprintf("%s%s  %d msgs  %s", cursor, age, e.MessageCount, e.ID[:8]))

		maxPreview := w - 4
		preview := e.Preview
		if len(preview) > maxPreview {
			preview = preview[:maxPreview-3] + "..."
		}
		previewLine := "    " + pStyle.Render(preview)

		sb.WriteString(meta + "\n" + previewLine + "\n")
	}

	if start > 0 {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above\n", start)))
	}
	if end < len(m.entries) {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below\n", len(m.entries)-end)))
	}

	return sb.String()
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		if days < 7 {
			return fmt.Sprintf("%dd ago", days)
		}
		return t.Format("Jan 2")
	}
}
