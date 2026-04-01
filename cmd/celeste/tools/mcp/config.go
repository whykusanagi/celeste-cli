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
