package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
)

// MemoryManagerModel is the interactive TUI for managing project memories.
type MemoryManagerModel struct {
	store     *memories.Store
	memories  []*memories.Memory
	cursor    int
	scroll    int
	width     int
	height    int
	confirmed int // index pending delete confirmation (-1 = none)
	expanded  int // index of fully expanded memory (-1 = none)
	message   string
}

// NewMemoryManagerModel creates a memory manager for the current project.
func NewMemoryManagerModel(workspace string) MemoryManagerModel {
	store := memories.NewStore(workspace)
	mems, _ := store.List()

	// Sort: stale first, then by type, then by name
	sort.Slice(mems, func(i, j int) bool {
		iDays, _ := memories.CheckStaleness(mems[i])
		jDays, _ := memories.CheckStaleness(mems[j])
		if iDays != jDays {
			return iDays > jDays // oldest first
		}
		if mems[i].Type != mems[j].Type {
			return mems[i].Type < mems[j].Type
		}
		return mems[i].Name < mems[j].Name
	})

	return MemoryManagerModel{
		store:     store,
		memories:  mems,
		confirmed: -1,
		expanded:  -1,
	}
}

func (m MemoryManagerModel) Init() tea.Cmd {
	return nil
}

func (m MemoryManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "esc":
			if m.confirmed >= 0 {
				m.confirmed = -1 // cancel delete
				m.message = ""
				return m, nil
			}
			return m, nil // parent handles exit

		case "up", "k":
			m.confirmed = -1
			m.message = ""
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scroll {
					m.scroll = m.cursor
				}
			}

		case "down", "j":
			m.confirmed = -1
			m.message = ""
			if m.cursor < len(m.memories)-1 {
				m.cursor++
				maxVis := m.maxVisible()
				if m.cursor >= m.scroll+maxVis {
					m.scroll++
				}
			}

		case "enter", " ":
			if len(m.memories) > 0 {
				if m.expanded == m.cursor {
					m.expanded = -1
				} else {
					m.expanded = m.cursor
				}
				m.confirmed = -1
			}

		case "d", "D":
			if len(m.memories) == 0 {
				return m, nil
			}
			if m.confirmed == m.cursor {
				// Second press = confirmed delete
				mem := m.memories[m.cursor]
				_ = m.store.Delete(mem.Name)
				// Remove from index
				indexPath := filepath.Join(m.store.BaseDir(), "MEMORY.md")
				if idx, err := memories.LoadIndex(indexPath); err == nil {
					_ = idx.Remove(mem.Name)
					_ = idx.Save()
				}
				// Remove from list
				m.memories = append(m.memories[:m.cursor], m.memories[m.cursor+1:]...)
				if m.cursor >= len(m.memories) && m.cursor > 0 {
					m.cursor--
				}
				m.confirmed = -1
				m.message = fmt.Sprintf("Deleted: %s", mem.Name)
			} else {
				m.confirmed = m.cursor
				m.message = "Press D again to confirm delete"
			}

		case "p":
			// Purge all stale memories (> 30 days)
			m.confirmed = -1
			purged := 0
			var kept []*memories.Memory
			for _, mem := range m.memories {
				days, _ := memories.CheckStaleness(mem)
				if days > 30 {
					_ = m.store.Delete(mem.Name)
					indexPath := filepath.Join(m.store.BaseDir(), "MEMORY.md")
					if idx, err := memories.LoadIndex(indexPath); err == nil {
						_ = idx.Remove(mem.Name)
						_ = idx.Save()
					}
					purged++
				} else {
					kept = append(kept, mem)
				}
			}
			m.memories = kept
			if purged > 0 {
				m.message = fmt.Sprintf("Purged %d stale memories (>30 days)", purged)
			} else {
				m.message = "No stale memories to purge"
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

func (m MemoryManagerModel) maxVisible() int {
	v := m.height - 10
	if v < 5 {
		v = 15
	}
	return v
}

func (m MemoryManagerModel) View() string {
	var sb strings.Builder
	width := m.width
	if width < 40 {
		width = 80
	}

	// Header
	sb.WriteString(memHeaderStyle.Render(
		fmt.Sprintf(" 🧠 MEMORIES (%d)", len(m.memories))))
	sb.WriteString("\n")

	// Stats line
	totalSize := 0
	typeCount := make(map[string]int)
	staleCount := 0
	for _, mem := range m.memories {
		totalSize += len(mem.Content)
		typeCount[mem.Type]++
		days, _ := memories.CheckStaleness(mem)
		if days > 7 {
			staleCount++
		}
	}

	stats := fmt.Sprintf(" project:%d  user:%d  feedback:%d  reference:%d",
		typeCount["project"], typeCount["user"], typeCount["feedback"], typeCount["reference"])
	if staleCount > 0 {
		stats += fmt.Sprintf("  ⚠ %d stale", staleCount)
	}
	sb.WriteString(memStatsStyle.Render(stats))
	sb.WriteString("\n\n")

	if len(m.memories) == 0 {
		sb.WriteString(memMutedStyle.Render("  No memories saved for this project.\n"))
		sb.WriteString(memMutedStyle.Render("  Celeste will save memories automatically during conversation.\n"))
		sb.WriteString(memMutedStyle.Render("  Or use: celeste remember \"<text>\"\n"))
	}

	// Memory list
	maxVis := m.maxVisible()
	endIdx := m.scroll + maxVis
	if endIdx > len(m.memories) {
		endIdx = len(m.memories)
	}

	for i := m.scroll; i < endIdx; i++ {
		mem := m.memories[i]
		isCursor := i == m.cursor
		days, warning := memories.CheckStaleness(mem)

		// Type badge
		badge := memTypeBadge(mem.Type)

		// Age indicator
		age := ""
		if days > 0 {
			age = fmt.Sprintf(" %dd", days)
		}

		// Staleness marker
		staleMarker := ""
		if warning != "" {
			staleMarker = " ⚠"
		}

		line := fmt.Sprintf(" %s %-30s  %s%s%s",
			badge, truncateStr(mem.Name, 30), truncateStr(mem.Description, 40), age, staleMarker)

		if isCursor {
			if m.confirmed == i {
				sb.WriteString(memDeleteStyle.Render(line))
			} else {
				sb.WriteString(memCursorStyle.Width(width - 2).Render(line))
			}
		} else if warning != "" {
			sb.WriteString(memStaleStyle.Render(line))
		} else {
			sb.WriteString(memItemStyle.Render(line))
		}
		sb.WriteString("\n")

		// Show content: full if expanded, one-line preview if just cursor
		if i == m.expanded && mem.Content != "" {
			// Full content view
			sb.WriteString(memPreviewStyle.Render("    ┌─────────────────────────────────────"))
			sb.WriteString("\n")
			for _, line := range strings.Split(mem.Content, "\n") {
				sb.WriteString(memPreviewStyle.Render("    │ " + line))
				sb.WriteString("\n")
			}
			sb.WriteString(memPreviewStyle.Render("    └─────────────────────────────────────"))
			sb.WriteString("\n")
		} else if isCursor && mem.Content != "" && m.expanded != m.cursor {
			preview := mem.Content
			if len(preview) > 100 {
				preview = preview[:97] + "..."
			}
			// Single line, replace newlines
			preview = strings.ReplaceAll(preview, "\n", " ")
			sb.WriteString(memPreviewStyle.Render("    " + preview))
			sb.WriteString("\n")
		}
	}

	// Message line
	if m.message != "" {
		sb.WriteString("\n")
		sb.WriteString(memMessageStyle.Render("  " + m.message))
	}

	// Footer
	sb.WriteString("\n\n")
	footer := " [↑/↓] Navigate  [Enter] View  [D] Delete  [P] Purge stale (>30d)  [Q/Esc] Back"
	sb.WriteString(memFooterStyle.Render(footer))

	return sb.String()
}

func memTypeBadge(t string) string {
	switch t {
	case "project":
		return memBadgeProjectStyle.Render("PRJ")
	case "user":
		return memBadgeUserStyle.Render("USR")
	case "feedback":
		return memBadgeFeedbackStyle.Render("FBK")
	case "reference":
		return memBadgeRefStyle.Render("REF")
	default:
		return memMutedStyle.Render("???")
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// Corrupted-theme memory manager styles
var (
	memHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ff4da6")).
			Background(lipgloss.Color("#1a1a2e"))

	memStatsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b5cf6"))

	memItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#b8afc8"))

	memCursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff4da6")).
			Bold(true).
			Background(lipgloss.Color("#1a1a2e"))

	memDeleteStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ef4444")).
			Bold(true).
			Strikethrough(true)

	memStaleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7a7085"))

	memPreviewStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5a4575")).
			Italic(true)

	memMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7a7085"))

	memMessageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#eab308")).
			Bold(true)

	memFooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5a4575"))

	memBadgeProjectStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b5cf6")).Bold(true)
	memBadgeUserStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00d4ff")).Bold(true)
	memBadgeFeedbackStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#22c55e")).Bold(true)
	memBadgeRefStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#eab308")).Bold(true)
)

// memoryManagerExitMsg signals the manager should close (used for checking os import)
func init() {
	// Ensure os import is used (for filepath operations in Update)
	_ = os.PathSeparator
}
