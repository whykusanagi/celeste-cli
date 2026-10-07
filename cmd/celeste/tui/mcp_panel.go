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

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
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

	approval MCPApproval // nil: no server needs approval
	// confirm is the server whose approval waits for a y (#411); "" when
	// no confirmation is showing. confirmText describes it.
	confirm     string
	confirmText string
}

// MCPApproval reports and records the approval of a workspace MCP server
// for the /mcp panel, in the same trust store the chat's launch prompt and
// `celeste mcp trust` use.
type MCPApproval interface {
	// State is "approved", "declined" or "pending" for a workspace
	// server, "" for one that needs no approval (a home config).
	State(name string, cfg mcp.ServerConfig) string
	// Describe is what the person must see before approving: the source
	// file, command, args and env names (never values), one per line,
	// already safe to print.
	Describe(name string, cfg mcp.ServerConfig) string
	// Approve records the approval.
	Approve(name string, cfg mcp.ServerConfig) error
}

// SetApproval injects the approval store, so the panel shows each
// workspace server's approval and can approve one after a confirmation.
func (m *MCPPanelModel) SetApproval(a MCPApproval) { m.approval = a }

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
	m.confirm = ""
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
		if m.approval != nil {
			row.Approval = m.approval.State(name, cfg)
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

// needsApproval reports whether row is a workspace server not approved
// (declined or pending): it connects only after a confirmation.
func (m MCPPanelModel) needsApproval(row *MCPServerInfo) bool {
	return m.approval != nil && (row.Approval == "declined" || row.Approval == "pending")
}

// askApproval shows the confirmation for row.
func (m MCPPanelModel) askApproval(row *MCPServerInfo) MCPPanelModel {
	m.confirm = row.Name
	m.confirmText = m.approval.Describe(row.Name, m.configs[row.Name])
	return m
}

// approveCmd records the approval, then connects the server.
func (m MCPPanelModel) approveCmd(name string) tea.Cmd {
	a, cfg, connect := m.approval, m.configs[name], m.connectCmd(name)
	return func() tea.Msg {
		if err := a.Approve(name, cfg); err != nil {
			return MCPConnectResultMsg{Name: name, Err: fmt.Errorf("not approved: %w", err)}
		}
		return connect()
	}
}

// Update handles messages for the MCP panel.
func (m MCPPanelModel) Update(msg tea.Msg) (MCPPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.active {
			break
		}
		if m.confirm != "" {
			// Only y approves; any other key (esc included) cancels and
			// keeps the panel open.
			name := m.confirm
			m.confirm, m.confirmText = "", ""
			if s := msg.String(); s == "y" || s == "Y" {
				return m, m.approveCmd(name)
			}
			return m, nil
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
		case "a":
			if row := m.current(); row != nil && m.needsApproval(row) {
				return m.askApproval(row), nil
			}
		case "c":
			if row := m.current(); row != nil && !row.Connected {
				if m.needsApproval(row) {
					return m.askApproval(row), nil
				}
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

	// The box spans the panel width: a 1-cell border each side around
	// rows padded (or cut) to the inner width, so every row keeps its
	// right edge.
	w := m.width
	if w < 44 {
		w = 44
	}
	// Never wider than the terminal, however narrow.
	if m.width > 0 && w > m.width {
		w = m.width
	}
	innerW := w - 2

	borderStyle := lipgloss.NewStyle().Foreground(ColorBorderPurple)
	titleStyle := lipgloss.NewStyle().Foreground(ColorPurpleNeon).Bold(true)
	connectedStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	disconnectedStyle := lipgloss.NewStyle().Foreground(ColorError)
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	infoStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)
	cursorStyle := lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true)

	edge := borderStyle.Render("│")
	row := func(content string) string { return edge + padRight(content, innerW) + edge }

	title := " MCP Servers "
	topFill := w - 3 - lipgloss.Width(title)
	if topFill < 0 {
		topFill = 0
	}
	lines := []string{borderStyle.Render("╭─") + titleStyle.Render(title) + borderStyle.Render(strings.Repeat("─", topFill)+"╮")}

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
			switch srv.Approval {
			case "declined":
				detail += " · declined (a approves)"
			case "pending":
				detail += " · not approved (a approves)"
			}
		}

		prefix := "  "
		srvName := nameStyle.Render(srv.Name)
		if i == m.cursor {
			prefix = cursorStyle.Render("> ")
			srvName = cursorStyle.Render(srv.Name)
		}
		lines = append(lines, row(fmt.Sprintf("%s %s  %s  %s", prefix, dot, srvName, infoStyle.Render(detail))))
	}

	if len(m.servers) == 0 {
		lines = append(lines, row("  "+infoStyle.Render("No MCP servers configured")))
	}

	if m.confirm != "" {
		warnStyle := lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true)
		lines = append(lines, row(""))
		lines = append(lines, row(warnStyle.Render(fmt.Sprintf("  Approve MCP server %q?", m.confirm))))
		for _, l := range strings.Split(m.confirmText, "\n") {
			lines = append(lines, row("    "+infoStyle.Render(l)))
		}
		lines = append(lines, row(infoStyle.Render("  Starting it runs this command with your permissions (or connects to this URL).")))
		lines = append(lines, row(warnStyle.Render("  y approve and connect · any other key cancels")))
	}

	lines = append(lines, row(""))
	lines = append(lines, row(infoStyle.Render(fmt.Sprintf("  Total: %d external tools available", totalTools))))
	// The keys are on the app's hint row (hintsFor): the panel repeats no
	// footer of its own (#398 C4).
	lines = append(lines, borderStyle.Render("╰"+strings.Repeat("─", innerW)+"╯"))

	return strings.Join(lines, "\n")
}
