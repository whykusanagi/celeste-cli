// cmd/celeste/tools/mcp/config.go
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

	// Trusted honours this server's readOnlyHint, which auto-approves the
	// tools it marks read-only. Set it only for servers you control. It is
	// read only from the home-level configs (GlobalConfigPaths): a
	// repository's .mcp.json cannot vouch for its own server (2.0 W4).
	Trusted bool `json:"trusted,omitempty"`

	// Dir is the working directory a stdio server starts in; empty is
	// celeste's own. Set by the caller (an ACP session uses its folder),
	// never parsed from JSON.
	Dir string `json:"-"`

	// Origin is the absolute path of the config file this server was loaded
	// from. Set by the discovery/merge layer, never parsed from JSON. Used by
	// the /mcp panel to show provenance.
	Origin string `json:"-"`
}

// LoadConfig reads and parses the MCP configuration from a JSON file.
// If the file does not exist, returns an empty config (not an error).
// This allows celeste to start without any MCP servers configured.
//
// Only a regular file of at most maxConfigBytes is read: a workspace's
// .mcp.json is untrusted, and a FIFO or a link to a device would block or
// never end (Aikido 806869859).
func LoadConfig(path string) (*MCPConfig, error) {
	data, err := readConfigFile(path)
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

// maxConfigBytes caps an MCP config file. Real ones are a few KiB.
const maxConfigBytes = 1 << 20

// readConfigFile reads path if it is a regular file of at most
// maxConfigBytes. It checks the file before opening it (opening a FIFO
// blocks), opens it non-blocking where the OS has that, and checks the
// opened file again.
func readConfigFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|openNonBlock, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ofi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !ofi.Mode().IsRegular() || !os.SameFile(fi, ofi) {
		return nil, fmt.Errorf("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("file too large (over %d bytes)", maxConfigBytes)
	}
	return data, nil
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
	return os.WriteFile(path, data, 0o600) // existing file: its mode is kept
}
