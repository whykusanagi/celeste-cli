// cmd/celeste/tools/mcp/manager.go
package mcp

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// ServerInfo contains health/status information for a connected MCP server.
type ServerInfo struct {
	Name      string
	Transport string
	Connected bool
	ToolCount int
}

// Manager handles the lifecycle of all MCP server connections.
// It loads configuration, connects to servers, discovers tools,
// and registers them in the tool registry.
type Manager struct {
	configPath string
	registry   *tools.Registry
	clients    map[string]*Client
	toolCounts map[string]int
	transports map[string]string
	mu         sync.Mutex
}

// NewManager creates a new MCP Manager.
// configPath is the path to the MCP configuration file (e.g., ~/.celeste/mcp.json).
// registry is the tool registry where discovered MCP tools will be registered.
func NewManager(configPath string, registry *tools.Registry) *Manager {
	return &Manager{
		configPath: configPath,
		registry:   registry,
		clients:    make(map[string]*Client),
		toolCounts: make(map[string]int),
		transports: make(map[string]string),
	}
}

// Start loads the MCP configuration, connects to all configured servers,
// discovers their tools, and registers them in the tool registry.
// If the config file does not exist, it returns nil (no MCP configured).
// If a server fails to connect, it logs a warning and continues with others.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		return fmt.Errorf("load MCP config: %w", err)
	}

	if len(cfg.Servers) == 0 {
		return nil
	}

	totalTools := 0
	connectedServers := 0

	for name, serverCfg := range cfg.Servers {
		transport, err := m.createTransport(serverCfg)
		if err != nil {
			log.Printf("[mcp] warning: failed to create transport for server %q: %v", name, err)
			continue
		}

		client := NewClient(transport, "celeste", "1.0")

		if err := client.Initialize(ctx); err != nil {
			log.Printf("[mcp] warning: failed to initialize server %q: %v", name, err)
			transport.Close()
			continue
		}

		toolsBefore := m.registry.Count()

		if err := DiscoverAndRegister(ctx, client, m.registry, name); err != nil {
			log.Printf("[mcp] warning: failed to discover tools from server %q: %v", name, err)
			client.Close()
			continue
		}

		toolsAfter := m.registry.Count()
		toolCount := toolsAfter - toolsBefore

		m.clients[name] = client
		m.toolCounts[name] = toolCount
		m.transports[name] = serverCfg.Transport
		totalTools += toolCount
		connectedServers++
	}

	if connectedServers > 0 {
		log.Printf("[mcp] connected to %d server(s), %d tool(s) discovered", connectedServers, totalTools)
	}

	return nil
}

// createTransport creates the appropriate Transport based on server configuration.
func (m *Manager) createTransport(cfg ServerConfig) (Transport, error) {
	switch cfg.Transport {
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("SSE transport requires a URL")
		}
		return NewSSETransport(cfg.URL)
	case "stdio", "":
		if cfg.Command == "" {
			return nil, fmt.Errorf("stdio transport requires a command")
		}
		return NewStdioTransport(cfg.Command, cfg.Args, cfg.Env)
	default:
		return nil, fmt.Errorf("unknown transport type: %q", cfg.Transport)
	}
}

// Stop gracefully disconnects from all MCP servers.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			log.Printf("[mcp] warning: error closing server %q: %v", name, err)
		}
	}

	m.clients = make(map[string]*Client)
	m.toolCounts = make(map[string]int)
	m.transports = make(map[string]string)

	return nil
}

// ServerStatus returns health information for all connected MCP servers.
func (m *Manager) ServerStatus() []ServerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var infos []ServerInfo
	for name, client := range m.clients {
		_ = client // client is stored to confirm connection
		infos = append(infos, ServerInfo{
			Name:      name,
			Transport: m.transports[name],
			Connected: true,
			ToolCount: m.toolCounts[name],
		})
	}
	return infos
}
