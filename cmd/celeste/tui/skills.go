package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// SkillsBrowserModel is the TUI model for the interactive skills browser. It
// type-ahead filters (BM25-ranked, same index as find_tools) and paginates, so
// a large tool list stays navigable instead of scrolling forever.
type SkillsBrowserModel struct {
	cursor        int    // index into filtered
	offset        int    // first visible filtered row (pagination window)
	query         string // type-ahead filter
	skillsList    []SkillDefinition
	filtered      []int // indices into skillsList, in display order
	width, height int
}

// skillsBrowserBackMsg asks the app to leave the skills view.
type skillsBrowserBackMsg struct{}

type skillSelectedMsg struct {
	skillName string
}

// NewSkillsBrowserModel creates the browser, sorted by name.
func NewSkillsBrowserModel(skillsList []SkillDefinition) SkillsBrowserModel {
	sort.Slice(skillsList, func(i, j int) bool {
		return skillsList[i].Name < skillsList[j].Name
	})
	m := SkillsBrowserModel{skillsList: skillsList}
	m.applyFilter()
	return m
}

// Init initializes the model (no-op).
func (m SkillsBrowserModel) Init() tea.Cmd { return nil }

// applyFilter recomputes the visible order from the query. Empty query = every
// skill in name order. Otherwise: BM25-ranked matches first (same index as
// find_tools), then a case-insensitive substring fallback so partial-word
// type-ahead still catches related tokens (e.g. "task" → "tasks_list").
func (m *SkillsBrowserModel) applyFilter() {
	m.cursor = 0
	m.offset = 0

	if strings.TrimSpace(m.query) == "" {
		m.filtered = make([]int, len(m.skillsList))
		for i := range m.skillsList {
			m.filtered[i] = i
		}
		return
	}

	docs := make([]string, len(m.skillsList))
	for i, s := range m.skillsList {
		docs[i] = s.Name + " " + s.Description
	}

	ranked := tools.RankDocs(docs, m.query) // BM25 hits, best first
	seen := make(map[int]bool, len(ranked))
	filtered := make([]int, 0, len(m.skillsList))
	for _, i := range ranked {
		seen[i] = true
		filtered = append(filtered, i)
	}

	lq := strings.ToLower(strings.TrimSpace(m.query))
	for i := range m.skillsList { // skillsList is name-sorted, so these stay ordered
		if seen[i] {
			continue
		}
		if strings.Contains(strings.ToLower(docs[i]), lq) {
			filtered = append(filtered, i)
		}
	}
	m.filtered = filtered
}

// pageSize is how many skill rows fit under the header/search and above the
// detail + position + footer. Falls back to a small window before the first
// WindowSizeMsg.
func (m SkillsBrowserModel) pageSize() int {
	ps := m.height - 9 // title, search, blank, 2-line detail, position, footer
	if ps < 3 {
		ps = 3
	}
	return ps
}

// descLines wraps a skill description to width, capped at maxLines. When text
// is dropped the last kept line ends with an ellipsis.
func descLines(desc string, width, maxLines int) []string {
	if width < 8 {
		width = 8
	}
	lines := strings.Split(wrapText(desc, width), "\n")
	if len(lines) > maxLines {
		last := lines[maxLines-1]
		lines = lines[:maxLines]
		lines[maxLines-1] = truncateLine(last+" …", width)
	}
	return lines
}

// clampScroll keeps the cursor in range and inside the visible page.
func (m *SkillsBrowserModel) clampScroll() {
	if m.cursor > len(m.filtered)-1 {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	ps := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+ps {
		m.offset = m.cursor - ps + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// Update handles messages.
func (m SkillsBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			// First esc clears an active filter; a second (or esc with no
			// filter) leaves the skills view.
			if m.query != "" {
				m.query = ""
				m.applyFilter()
				return m, nil
			}
			return m, func() tea.Msg { return skillsBrowserBackMsg{} }
		case tea.KeyEnter:
			if len(m.filtered) > 0 {
				name := m.skillsList[m.filtered[m.cursor]].Name
				return m, func() tea.Msg { return skillSelectedMsg{skillName: name} }
			}
		case tea.KeyUp:
			m.cursor--
			m.clampScroll()
		case tea.KeyDown:
			m.cursor++
			m.clampScroll()
		case tea.KeyPgUp:
			m.cursor -= m.pageSize()
			m.clampScroll()
		case tea.KeyPgDown:
			m.cursor += m.pageSize()
			m.clampScroll()
		case tea.KeyHome:
			m.cursor = 0
			m.clampScroll()
		case tea.KeyEnd:
			m.cursor = len(m.filtered) - 1
			m.clampScroll()
		case tea.KeyBackspace:
			if m.query != "" {
				m.query = m.query[:len(m.query)-1]
				m.applyFilter()
			}
		case tea.KeySpace:
			m.query += " "
			m.applyFilter()
		case tea.KeyRunes:
			m.query += string(msg.Runes)
			m.applyFilter()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampScroll()
	}
	return m, nil
}

// View renders the browser.
func (m SkillsBrowserModel) View() string {
	var b strings.Builder
	muted := lipgloss.NewStyle().Foreground(ColorTextMuted)

	// Title + count (filtered / total).
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPurple).
		Render(fmt.Sprintf("Available Skills (%d/%d)", len(m.filtered), len(m.skillsList))) + "\n")

	// Search line.
	q := muted.Render("(type to search…)")
	if m.query != "" {
		q = lipgloss.NewStyle().Foreground(ColorAccentGlow).Render(m.query + "▌")
	}
	b.WriteString(muted.Render("Filter: ") + q + "\n\n")

	if len(m.filtered) == 0 {
		b.WriteString(muted.Render("  no skills match \""+m.query+"\" — Esc to clear") + "\n")
	}

	// Description column fills the terminal width past the fixed name column
	// (2 cursor + 25 name + 2 gutter = 29), so wide terminals show more of it.
	descWidth := m.width - 29
	if descWidth < 24 {
		descWidth = 24
	}

	ps := m.pageSize()
	end := m.offset + ps
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	for i := m.offset; i < end; i++ {
		skill := m.skillsList[m.filtered[i]]
		cursor := "  "
		if i == m.cursor {
			cursor = "› "
		}
		desc := truncateLine(skill.Description, descWidth)
		if i == m.cursor {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true).
				Render(fmt.Sprintf("%s%-25s  %s", cursor, skill.Name, desc)))
		} else {
			name := lipgloss.NewStyle().Foreground(ColorPurple).Render(fmt.Sprintf("%s%-25s", cursor, skill.Name))
			b.WriteString(name + lipgloss.NewStyle().Foreground(ColorPurpleDeep).Render("  "+desc))
		}
		b.WriteString("\n")
	}

	// Full description of the highlighted skill (list rows are truncated).
	// Always two lines so the layout height is stable across selections.
	detail := []string{"", ""}
	if len(m.filtered) > 0 {
		w := m.width - 2
		if w < 20 {
			w = 20
		}
		for i, ln := range descLines(m.skillsList[m.filtered[m.cursor]].Description, w, 2) {
			detail[i] = ln
		}
	}
	detailStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)
	b.WriteString("\n")
	for _, ln := range detail {
		b.WriteString(detailStyle.Render(" "+ln) + "\n")
	}

	// Position + scroll indicators.
	pos := ""
	if len(m.filtered) > 0 {
		pos = fmt.Sprintf("%d–%d of %d", m.offset+1, end, len(m.filtered))
	}
	if m.offset > 0 {
		pos += fmt.Sprintf("   ↑ %d more", m.offset)
	}
	if below := len(m.filtered) - end; below > 0 {
		pos += fmt.Sprintf("   ↓ %d more", below)
	}
	b.WriteString("\n" + muted.Render(pos) + "\n")

	b.WriteString(muted.Render("type to filter · ↑/↓ move · PgUp/PgDn page · Enter select · Esc clear/back"))
	return b.String()
}
