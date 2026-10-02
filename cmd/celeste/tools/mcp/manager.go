// cmd/celeste/tools/mcp/manager.go
package mcp

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	configPath  string
	configPaths []string // multi-source discovery paths; if set, Start uses LoadMerged
	registry    *tools.Registry
	clients     map[string]*Client
	toolCounts  map[string]int
	transports  map[string]string
	toolNames   map[string][]string // per-server registered tool names, for exact Disconnect
	origins     map[string]string   // per-server config file (ServerConfig.Origin), for RegisterGlobalInto
	home        string              // the user's home: its configs alone may set "trusted"
	mu          sync.Mutex
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
		toolNames:  make(map[string][]string),
		origins:    make(map[string]string),
		home:       userHome(),
	}
}

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

// trusts reports whether cfg's "trusted": true is honoured: only when it
// came from one of the home-level configs (GlobalConfigPaths). A server of
// unknown origin, or from a workspace's .mcp.json or .celeste/mcp.json,
// cannot vouch for itself (2.0 W4).
func (m *Manager) trusts(cfg ServerConfig) bool {
	if !cfg.Trusted || cfg.Origin == "" || m.home == "" {
		return false
	}
	return isGlobalConfig(m.home, cfg.Origin)
}

// isGlobalConfig reports whether path is one of home's GlobalConfigPaths.
func isGlobalConfig(home, path string) bool {
	for _, p := range GlobalConfigPaths(home) {
		if filepath.Clean(p) == filepath.Clean(path) {
			return true
		}
	}
	return false
}

// NewManagerMulti creates a Manager that merges MCP config from multiple
// discovered paths (see DiscoverConfigPaths / LoadMerged) instead of a single
// file. Lower-index paths are overridden by higher-index paths on name clash.
func NewManagerMulti(paths []string, registry *tools.Registry) *Manager {
	m := NewManager("", registry)
	m.configPaths = paths
	return m
}

// Start loads the MCP configuration, connects to all configured servers,
// discovers their tools, and registers them in the tool registry.
// If the config file does not exist, it returns nil (no MCP configured).
// If a server fails to connect, it logs a warning and continues with others.
func (m *Manager) Start(ctx context.Context) error {
	var cfg *MCPConfig
	var err error
	if len(m.configPaths) > 0 {
		cfg, err = LoadMerged(m.configPaths)
	} else {
		cfg, err = LoadConfig(m.configPath)
	}
	if err != nil {
		return fmt.Errorf("load MCP config: %w", err)
	}

	if len(cfg.Servers) == 0 {
		return nil
	}

	totalTools := 0
	connectedServers := 0

	for name, serverCfg := range cfg.Servers {
		// Opt-in: skip servers not explicitly enabled before spawning any
		// process or opening any connection, so they cost nothing at startup.
		if !serverCfg.Enabled {
			continue
		}
		if err := m.Connect(ctx, name, serverCfg); err != nil {
			log.Printf("[mcp] warning: %v", err)
			continue
		}
		m.mu.Lock()
		totalTools += m.toolCounts[name]
		m.mu.Unlock()
		connectedServers++
	}

	if connectedServers > 0 {
		log.Printf("[mcp] connected to %d server(s), %d tool(s) discovered", connectedServers, totalTools)
	}

	return nil
}

// connectClient initializes an already-built client, discovers + registers its
// tools, and records bookkeeping. The caller holds no lock; connectClient locks
// only while mutating manager maps. trusted honours the tools' readOnlyHint.
func (m *Manager) connectClient(ctx context.Context, name string, client *Client, transport string, trusted bool) error {
	if err := client.Initialize(ctx); err != nil {
		client.Close()
		return fmt.Errorf("initialize %q: %w", name, err)
	}
	names, err := discoverAndRegister(ctx, client, m.registry, name, m.liveClient, trusted)
	if err != nil {
		client.Close()
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[name] = client
	m.toolCounts[name] = len(names)
	m.transports[name] = transport
	m.toolNames[name] = names
	return nil
}

// Connect builds a transport from cfg, connects, and registers the server's
// tools at runtime. Safe to call after Start (lazy connect). A server that is
// already connected is a no-op.
func (m *Manager) Connect(ctx context.Context, name string, cfg ServerConfig) error {
	m.mu.Lock()
	_, already := m.clients[name]
	m.mu.Unlock()
	if already {
		return nil
	}
	transport, err := m.createTransport(cfg)
	if err != nil {
		return fmt.Errorf("create transport for %q: %w", name, err)
	}
	if err := m.connectClient(ctx, name, NewClient(transport, "celeste", "1.0"), cfg.Transport, m.trusts(cfg)); err != nil {
		return err
	}
	m.mu.Lock()
	m.origins[name] = cfg.Origin
	m.mu.Unlock()
	return nil
}

// liveClient is the server's current client, if it is connected. A tool
// the manager registered calls through it, so a copy of the tool held by a
// nested run's registry follows a reconnect (2.0 F2e).
func (m *Manager) liveClient(name string) (*Client, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[name]
	return c, ok
}

// IsConnected reports whether a server currently has a live client.
func (m *Manager) IsConnected(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.clients[name]
	return ok
}

// Disconnect closes a server's client and removes exactly the tools it added.
// No-op if the server is not connected.
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	client, ok := m.clients[name]
	names := m.toolNames[name]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.clients, name)
	delete(m.toolCounts, name)
	delete(m.transports, name)
	delete(m.toolNames, name)
	delete(m.origins, name)
	m.mu.Unlock()

	for _, tn := range names {
		m.registry.Unregister(tn)
	}
	return client.Close()
}

// RegisterInto registers every connected server's tools in dst, hidden until
// find_tools activates them (as discoverAndRegister does), and returns how
// many it added. A nested run's registry uses it to share this manager's
// clients instead of starting its own servers. Tools a server adds later are
// not mirrored; the nested registry is a snapshot.
func (m *Manager) RegisterInto(dst *tools.Registry) int {
	return m.registerInto(dst, func(string) bool { return true })
}

// RegisterGlobalInto is RegisterInto for a non-interactive run nested under
// the chat (2.0 F2e): only servers configured in one of home's global files
// (GlobalConfigPaths) are mirrored. A server from the workspace's
// .mcp.json or .celeste/mcp.json, or of unknown origin, stays the chat's:
// non-interactive runs never run a repo's MCP servers.
func (m *Manager) RegisterGlobalInto(dst *tools.Registry, home string) int {
	global := map[string]bool{}
	for _, p := range GlobalConfigPaths(home) {
		global[filepath.Clean(p)] = true
	}
	m.mu.Lock()
	origins := make(map[string]string, len(m.origins))
	for name, o := range m.origins {
		origins[name] = o
	}
	m.mu.Unlock()
	return m.registerInto(dst, func(server string) bool {
		o := origins[server]
		return o != "" && global[filepath.Clean(o)]
	})
}

func (m *Manager) registerInto(dst *tools.Registry, keep func(server string) bool) int {
	m.mu.Lock()
	var names []string
	for server, ns := range m.toolNames {
		if keep(server) {
			names = append(names, ns...)
		}
	}
	m.mu.Unlock()
	n := 0
	for _, name := range names {
		if t, ok := m.registry.Get(name); ok {
			// Add, never Register: a name dst already holds stays its own.
			if err := dst.Add(t); err != nil {
				log.Printf("[mcp] %v", err)
				continue
			}
			dst.SetHidden(name, true)
			n++
		}
	}
	return n
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
	case "http":
		if cfg.URL == "" {
			return nil, fmt.Errorf("HTTP transport requires a URL")
		}
		tr, err := NewHTTPTransport(cfg.URL)
		if err != nil {
			return nil, err
		}
		tr.SetProtocolVersion(preferredProtocolVersion)
		return tr, nil
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
	// Closed servers' tools are no longer mirrored into nested runs.
	m.toolNames = make(map[string][]string)
	m.origins = make(map[string]string)

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
