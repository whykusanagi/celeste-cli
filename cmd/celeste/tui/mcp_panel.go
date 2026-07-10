// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the MCP server status panel, accessed via /mcp command.
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

// MCPPanelModel displays MCP server connection status and tool counts, and
// dispatches runtime connect/disconnect/toggle actions.
type MCPPanelModel struct {
	active  bool
	servers []MCPServerInfo
	cursor  int
	width   int
	height  int

	manager *mcp.Manager
	configs map[string]mcp.ServerConfig // discovered server configs, keyed by name
}

// NewMCPPanelModel creates a new MCP panel model.
func NewMCPPanelModel() MCPPanelModel {
	return MCPPanelModel{}
}

// Init initializes the model (no-op).
func (m MCPPanelModel) Init() tea.Cmd {
	return nil
}

// SetSize updates the component dimensions.
func (m *MCPPanelModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Active returns whether the MCP panel is currently displayed.
func (m MCPPanelModel) Active() bool {
	return m.active
}

// SetManager injects the MCP manager and discovered configs so the panel can
// connect/disconnect servers and toggle their enabled flag at runtime.
func (m *MCPPanelModel) SetManager(manager *mcp.Manager, configs map[string]mcp.ServerConfig) {
	m.manager = manager
	m.configs = configs
}

// Show activates the MCP panel and refreshes its rows from live state.
func (m *MCPPanelModel) Show() {
	m.active = true
	m.cursor = 0
	m.servers = m.rowsFromStatus()
}

// rowsFromStatus merges live server status with the discovered configs so both
// connected and configured-but-disconnected servers appear, sorted by name.
func (m MCPPanelModel) rowsFromStatus() []MCPServerInfo {
	connected := map[string]mcp.ServerInfo{}
	if m.manager != nil {
		for _, s := range m.manager.ServerStatus() {
			connected[s.Name] = s
		}
	}

	rows := make([]MCPServerInfo, 0, len(m.configs))
	for name, cfg := range m.configs {
		row := MCPServerInfo{
			Name:      name,
			Transport: cfg.Transport,
			Enabled:   cfg.Enabled,
			Origin:    cfg.Origin,
		}
		if s, ok := connected[name]; ok {
			row.Connected = true
			row.ToolCount = s.ToolCount
			row.Transport = s.Transport
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// RefreshServers rebuilds the panel rows from current manager state.
func (m MCPPanelModel) RefreshServers() MCPPanelModel {
	m.servers = m.rowsFromStatus()
	if m.cursor >= len(m.servers) {
		m.cursor = 0
	}
	return m
}

// current returns the selected server row, or nil when there is none.
func (m MCPPanelModel) current() *MCPServerInfo {
	if m.cursor < 0 || m.cursor >= len(m.servers) {
		return nil
	}
	return &m.servers[m.cursor]
}

// connectCmd dispatches an async connect (runs off the Update loop, so an OAuth
// handshake can block safely). 60s is the ceiling for a first-login handshake.
func (m MCPPanelModel) connectCmd(name string) tea.Cmd {
	mgr := m.manager
	cfg := m.configs[name]
	return func() tea.Msg {
		if mgr == nil {
			return MCPConnectResultMsg{Name: name, Err: fmt.Errorf("no MCP manager")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return MCPConnectResultMsg{Name: name, Err: mgr.Connect(ctx, name, cfg)}
	}
}

// disconnectCmd dispatches an async disconnect.
func (m MCPPanelModel) disconnectCmd(name string) tea.Cmd {
	mgr := m.manager
	return func() tea.Msg {
		if mgr == nil {
			return MCPConnectResultMsg{Name: name, Err: fmt.Errorf("no MCP manager")}
		}
		return MCPConnectResultMsg{Name: name, Err: mgr.Disconnect(name)}
	}
}

// toggleEnabledCmd persists the enabled flag to the server's owning config file.
// Only files celeste can write (its Origin) are affected.
func (m MCPPanelModel) toggleEnabledCmd(name string, enabled bool) tea.Cmd {
	path := m.configs[name].Origin
	return func() tea.Msg {
		if path == "" {
			return MCPConnectResultMsg{Name: name, Err: fmt.Errorf("no config file to write for %q", name)}
		}
		return MCPConnectResultMsg{Name: name, Err: mcp.SetServerEnabled(path, name, enabled)}
	}
}

// Update handles messages for the MCP panel.
func (m MCPPanelModel) Update(msg tea.Msg) (MCPPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case MCPStatusMsg:
		m.servers = msg.Servers

	case tea.KeyMsg:
		if !m.active {
			break
		}
		switch msg.String() {
		case "esc", "q":
			m.active = false
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.servers)-1 {
				m.cursor++
			}
		case "c":
			if row := m.current(); row != nil && !row.Connected {
				return m, m.connectCmd(row.Name)
			}
		case "d":
			if row := m.current(); row != nil && row.Connected {
				return m, m.disconnectCmd(row.Name)
			}
		case "r":
			if row := m.current(); row != nil {
				return m, tea.Sequence(m.disconnectCmd(row.Name), m.connectCmd(row.Name))
			}
		case " ":
			if row := m.current(); row != nil {
				return m, m.toggleEnabledCmd(row.Name, !row.Enabled)
			}
		}
	}
	return m, nil
}

// View renders the MCP panel.
func (m MCPPanelModel) View() string {
	if !m.active {
		return ""
	}

	w := m.width
	if w < 40 {
		w = 44
	}
	innerW := w - 6
	if innerW < 30 {
		innerW = 30
	}

	borderStyle := lipgloss.NewStyle().Foreground(ColorBorderPurple)
	titleStyle := lipgloss.NewStyle().Foreground(ColorPurpleNeon).Bold(true)
	connectedStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	disconnectedStyle := lipgloss.NewStyle().Foreground(ColorError)
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	infoStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)
	cursorStyle := lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true)
	footerStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)

	hRule := strings.Repeat("─", innerW)
	title := " MCP Servers "

	// Top border
	topFill := innerW - len(title)
	if topFill < 0 {
		topFill = 0
	}
	top := borderStyle.Render("╭─") + titleStyle.Render(title) + borderStyle.Render(strings.Repeat("─", topFill)+"╮")

	// Server lines
	var lines []string
	lines = append(lines, top)

	totalTools := 0
	for i, srv := range m.servers {
		var dot string
		var detail string
		if srv.Connected {
			dot = connectedStyle.Render("●")
			detail = fmt.Sprintf("%d tools    %s", srv.ToolCount, srv.Transport)
			totalTools += srv.ToolCount
		} else {
			dot = disconnectedStyle.Render("○")
			if srv.Enabled {
				detail = "enabled · disconnected"
			} else {
				detail = "disabled"
			}
		}

		prefix := "  "
		srvName := nameStyle.Render(srv.Name)
		if i == m.cursor {
			prefix = cursorStyle.Render("> ")
			srvName = cursorStyle.Render(srv.Name)
		}

		line := fmt.Sprintf("%s%s %s  %s  %s",
			borderStyle.Render("│"),
			prefix,
			dot,
			srvName,
			infoStyle.Render(detail),
		)
		lines = append(lines, line)
	}

	if len(m.servers) == 0 {
		lines = append(lines, borderStyle.Render("│")+"  "+infoStyle.Render("No MCP servers configured"))
	}

	// Blank + summary line
	lines = append(lines, borderStyle.Render("│"))
	summaryText := fmt.Sprintf("  Total: %d external tools available", totalTools)
	lines = append(lines, borderStyle.Render("│")+infoStyle.Render(summaryText))

	// Bottom border
	bot := borderStyle.Render("╰" + hRule + "──╯")
	lines = append(lines, bot)

	// Footer with keybindings
	lines = append(lines, footerStyle.Render("[↑/↓] Nav  [c] Connect  [d] Disconnect  [r] Reconnect  [Space] Toggle  [Esc] Close"))

	return strings.Join(lines, "\n")
}
