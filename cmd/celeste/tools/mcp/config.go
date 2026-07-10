// cmd/celeste/tools/mcp/config.go
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MCPConfig is the top-level configuration for MCP servers.
// Loaded from ~/.celeste/mcp.json.
type MCPConfig struct {
	Servers map[string]ServerConfig `json:"mcpServers"`
}

// ServerConfig defines how to connect to a single MCP server.
type ServerConfig struct {
	// Enabled opts the server into auto-connection at startup. It is false by
	// default: a server is only connected if it explicitly sets
	// "enabled": true. This keeps configured-but-unused servers (e.g. an X
	// bridge awaiting its first OAuth login) from delaying startup.
	Enabled bool `json:"enabled,omitempty"`

	// Transport is "stdio" or "sse". Defaults to "stdio" if not set.
	Transport string `json:"transport"`

	// Command is the executable to spawn (stdio transport only).
	Command string `json:"command,omitempty"`

	// Args are command-line arguments (stdio transport only).
	Args []string `json:"args,omitempty"`

	// URL is the SSE endpoint URL (sse transport only).
	URL string `json:"url,omitempty"`

	// Env is a map of environment variables passed to the child process.
	// Values support ${VAR} expansion from the host environment.
	Env map[string]string `json:"env,omitempty"`

	// Origin is the absolute path of the config file this server was loaded
	// from. Set by the discovery/merge layer, never parsed from JSON. Used by
	// the /mcp panel to show provenance.
	Origin string `json:"-"`
}

// DefaultConfigPath returns the default path for the MCP configuration file.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".celeste", "mcp.json")
}

// LoadConfig reads and parses the MCP configuration from a JSON file.
// If the file does not exist, returns an empty config (not an error).
// This allows celeste to start without any MCP servers configured.
func LoadConfig(path string) (*MCPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &MCPConfig{Servers: make(map[string]ServerConfig)}, nil
		}
		return nil, fmt.Errorf("read MCP config %s: %w", path, err)
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse MCP config %s: %w", path, err)
	}

	if config.Servers == nil {
		config.Servers = make(map[string]ServerConfig)
	}

	// Apply defaults
	for name, server := range config.Servers {
		if server.Transport == "" {
			server.Transport = "stdio"
			config.Servers[name] = server
		}
	}

	return &config, nil
}

// SetServerEnabled flips the `enabled` flag of one server in the config file at
// path and writes it back, preserving all other fields. Errors if the file or
// the named server does not exist.
func SetServerEnabled(path, name string, enabled bool) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}
	sc, ok := cfg.Servers[name]
	if !ok {
		return fmt.Errorf("server %q not found in %s", name, path)
	}
	sc.Enabled = enabled
	sc.Origin = "" // never serialize
	cfg.Servers[name] = sc

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal MCP config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
