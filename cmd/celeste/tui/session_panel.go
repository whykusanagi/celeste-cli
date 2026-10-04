// Session picker panel — browsable list of saved sessions.
// Replaces the text-dump /session list with an interactive UI.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// SessionEntry is a display-ready session summary.
type SessionEntry struct {
	ID           string
	Name         string // set by /session rename, /fork and the first prompt
	Preview      string // first user message, truncated
	MessageCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ThisProject  bool // ran in the chat's project (2.0 W4 ruling 2)
}

// SessionPanelModel is the interactive session picker.
type SessionPanelModel struct {
	entries  []SessionEntry
	cursor   int
	width    int
	height   int
	selected string // ID of selected session (empty = none)
	deleted  string // ID of deleted session
	current  string // ID of the chat's own session: marked, never deleted
	notice   string // shown in place of the key hints until the next key
	err      error
}

// NewSessionPanelModel loads sessions and creates the picker; workDir's
// project's sessions come first.
func NewSessionPanelModel(workDir string) SessionPanelModel {
	m := SessionPanelModel{}
	m.loadSessions(workDir)
	return m
}

func (m *SessionPanelModel) loadSessions(workDir string) {
	mgr := config.NewSessionManager()
	sessions, err := mgr.List()
	if err != nil {
		m.err = err
		return
	}

	mine, others := config.SortForWorkspace(sessions, workDir)
	sessions = append(mine, others...)
	// Every session /session list shows, the ones with no messages yet
	// included (a fresh fork, a new chat), so the two never disagree.
	m.entries = make([]SessionEntry, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]

		entry := SessionEntry{
			ID:           s.ID,
			Name:         s.Name,
			MessageCount: len(s.Messages),
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
			ThisProject:  i < len(mine),
		}

		// Extract first user message as preview
		for _, msg := range s.Messages {
			if msg.Role == "user" && strings.TrimSpace(msg.Content) != "" {
				entry.Preview = strings.Join(strings.Fields(msg.Content), " ")
				break
			}
		}
		if entry.Preview == "" {
			if len(s.Messages) == 0 {
				entry.Preview = "(no messages yet)"
			} else {
				entry.Preview = "(no user messages)"
			}
		}

		m.entries = append(m.entries, entry)
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

// WithCurrent marks id as the chat's own session: its row says (current)
// and d or Backspace on it deletes nothing.
func (m SessionPanelModel) WithCurrent(id string) SessionPanelModel {
	m.current = id
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
		m.notice = ""
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
			m.cursor -= m.pageSize()
			if m.cursor < 0 {
				m.cursor = 0
			}
		case "pgdown":
			m.cursor += m.pageSize()
			if m.cursor >= len(m.entries) {
				m.cursor = max(len(m.entries)-1, 0)
			}
		case "enter":
			if len(m.entries) > 0 {
				m.selected = m.entries[m.cursor].ID
			}
		case "d", "delete", "backspace":
			if len(m.entries) > 0 && m.current != "" && m.entries[m.cursor].ID == m.current {
				m.notice = "That is the current session; switch away from it to delete it."
			} else if len(m.entries) > 0 {
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

// View renders the session picker. It fills the height it was given, so
// the input and the bars below it stay at the bottom of the screen.
func (m SessionPanelModel) View() string {
	return m.fill(m.body())
}

// fill pads (or cuts) the picker to its height and cuts each row to its width.
func (m SessionPanelModel) fill(lines []string) string {
	if m.height > 0 {
		if len(lines) > m.height {
			lines = lines[:m.height]
		}
		for len(lines) < m.height {
			lines = append(lines, "")
		}
	}
	if m.width > 0 {
		for i, ln := range lines {
			lines[i] = fitWidth(ln, m.width)
		}
	}
	return strings.Join(lines, "\n")
}

// sessionRowsPerEntry is one meta row (age, messages, ID) and one preview row.
const sessionRowsPerEntry = 2

// sessionChromeRows are the 3 title rows and the 2 "more" rows.
const sessionChromeRows = 5

// pageSize is how many entries the picker shows at once; PgUp/PgDn move
// the cursor by that much.
func (m SessionPanelModel) pageSize() int {
	if m.height <= 0 {
		return 10
	}
	return max((m.height-sessionChromeRows)/sessionRowsPerEntry, 1)
}

func (m SessionPanelModel) body() []string {
	if m.err != nil {
		return []string{fmt.Sprintf("Error loading sessions: %v", m.err)}
	}

	if len(m.entries) == 0 {
		return []string{"No saved sessions.", "", "Press q or Esc to close."}
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(ColorPurple).
		Bold(true)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6b7280"))

	hint := hintStyle.Render("↑/↓ navigate  PgUp/PgDn page  Enter resume  d delete  Esc close")
	if m.notice != "" {
		hint = lipgloss.NewStyle().Foreground(ColorError).Render(m.notice)
	}
	lines := []string{
		titleStyle.Render(fmt.Sprintf("Sessions (%d)", len(m.entries))),
		hint,
		"",
	}

	// A page of entries around the cursor.
	pageSize := m.pageSize()

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

	previewW := m.width - 4
	if previewW < 20 {
		previewW = 76
	}

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

		here := ""
		if e.ThisProject {
			here = "  this project"
		}
		if m.current != "" && e.ID == m.current {
			here += "  (current)"
		}
		// The whole ID: rows from one day share their leading digits, and
		// /session resume takes the ID as shown.
		lines = append(lines, style.Render(fmt.Sprintf("%s%s  %d msgs  %s%s", cursor, formatAge(e.UpdatedAt), e.MessageCount, e.ID, here)))

		preview := e.Preview
		if e.Name != "" && e.Name != e.Preview {
			preview = e.Name + " · " + e.Preview
		}
		lines = append(lines, "    "+pStyle.Render(fitWidth(preview, previewW)))
	}

	if start > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
	}
	if end < len(m.entries) {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ↓ %d more below", len(m.entries)-end)))
	}
	return lines
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
