package mcp

import (
	"os"
	"path/filepath"
)

// DiscoverConfigPaths returns the MCP config files that exist on disk, ordered
// from lowest to highest precedence. Later paths override earlier ones on
// server-name collision (see LoadMerged). Non-existent candidates are skipped,
// so an empty slice means no MCP config anywhere.
func DiscoverConfigPaths(cwd, home string) []string {
	candidates := []string{
		filepath.Join(home, ".celeste", "mcp.json"),
		filepath.Join(home, ".claude", "mcp.json"),
		filepath.Join(home, ".cursor", "mcp.json"),
		filepath.Join(cwd, ".mcp.json"),
		filepath.Join(cwd, ".celeste", "mcp.json"),
	}

	var found []string
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			found = append(found, p)
		}
	}
	return found
}

// LoadMerged folds every config in paths into a single MCPConfig. paths must be
// ordered lowest-to-highest precedence (as DiscoverConfigPaths returns them):
// a server name present in a later path replaces the earlier definition
// wholesale. Each surviving server is stamped with the Origin path it came from.
// A missing file is skipped (LoadConfig already treats os.ErrNotExist as empty).
func LoadMerged(paths []string) (*MCPConfig, error) {
	merged := &MCPConfig{Servers: make(map[string]ServerConfig)}

	for _, p := range paths {
		cfg, err := LoadConfig(p) // applies default transport, tolerates missing
		if err != nil {
			return nil, err
		}
		for name, sc := range cfg.Servers {
			sc.Origin = p
			merged.Servers[name] = sc
		}
	}

	return merged, nil
}
