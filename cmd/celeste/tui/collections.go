package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/collections"
)

// CollectionsModel is the TUI model for collections management
type CollectionsModel struct {
	collections   []collections.Collection
	activeIDs     map[string]bool
	cursor        int
	viewport      viewport.Model
	manager       *collections.Manager
	width, height int
	err           error
}

// NewCollectionsModel creates a new collections model
func NewCollectionsModel(manager *collections.Manager) CollectionsModel {
	return CollectionsModel{
		manager:   manager,
		activeIDs: make(map[string]bool),
		viewport:  viewport.New(80, 20),
	}
}

// Init initializes the model
func (m CollectionsModel) Init() tea.Cmd {
	return m.loadCollections
}

// loadCollections fetches collections from API asynchronously
func (m CollectionsModel) loadCollections() tea.Msg {
	// Fetch collections from API
	cols, err := m.manager.ListCollections()
	if err != nil {
		return collectionsLoadedMsg{
			collections: nil,
			err:         err,
		}
	}

	return collectionsLoadedMsg{
		collections: cols,
		err:         nil,
	}
}

type collectionsLoadedMsg struct {
	collections []collections.Collection
	err         error
}

// Update handles messages
func (m CollectionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "esc":
			// Return to chat
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.collections)-1 {
				m.cursor++
			}
		case " ": // Toggle active/inactive
			if m.cursor < len(m.collections) {
				collectionID := m.collections[m.cursor].ID
				if m.activeIDs[collectionID] {
					if err := m.manager.DisableCollection(collectionID); err != nil {
						LogInfo(fmt.Sprintf("Error disabling collection: %v", err))
					}
					delete(m.activeIDs, collectionID)
				} else {
					if err := m.manager.EnableCollection(collectionID); err != nil {
						LogInfo(fmt.Sprintf("Error enabling collection: %v", err))
					}
					m.activeIDs[collectionID] = true
				}
				// Save config to persist changes
				if err := m.manager.SaveConfig(); err != nil {
					LogInfo(fmt.Sprintf("Error saving config: %v", err))
				}
			}
		}

	case collectionsLoadedMsg:
		m.collections = msg.collections
		m.err = msg.err

		// Load active collections using Manager method
		m.activeIDs = m.manager.GetActiveCollectionIDs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 4 // Reserve space for header/footer
	}

	return m, nil
}

// View renders the model
func (m CollectionsModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress 'q' to return to chat.", m.err)
	}

	if len(m.collections) == 0 {
		return "No collections found.\n\nPress 'q' to return to chat."
	}

	// Render collections list
	var content string

	// Active collections
	activeCount := 0
	for _, col := range m.collections {
		if m.activeIDs[col.ID] {
			activeCount++
		}
	}

	content += lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#8b5cf6")). // Purple (corrupted theme)
		Render(fmt.Sprintf("Active Collections (%d):", activeCount)) + "\n\n"

	for i, col := range m.collections {
		if !m.activeIDs[col.ID] {
			continue
		}

		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		line := fmt.Sprintf("%s✓ %-30s (%d docs)", cursor, col.Name, col.DocumentCount)
		if i == m.cursor {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d946ef")). // Bright pink (corrupted theme - cursor)
				Bold(true).
				Render(line)
		}
		content += line + "\n"
	}

	content += "\n"

	// Available collections
	inactiveCount := len(m.collections) - activeCount
	if inactiveCount > 0 {
		content += lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7c3aed")). // Dark purple (corrupted theme)
			Render(fmt.Sprintf("Available Collections (%d):", inactiveCount)) + "\n\n"

		for i, col := range m.collections {
			if m.activeIDs[col.ID] {
				continue
			}

			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}

			line := fmt.Sprintf("%s○ %-30s (%d docs)", cursor, col.Name, col.DocumentCount)
			if i == m.cursor {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#d946ef")). // Bright pink (corrupted theme - cursor)
					Bold(true).
					Render(line)
			}
			content += line + "\n"
		}
	}

	// Footer with keybindings
	footer := "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6d28d9")). // Dark purple (corrupted theme - muted)
		Render("[↑/↓] Navigate  [Space] Toggle  [Q] Back to Chat")

	return content + footer
}
