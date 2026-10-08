package mcp

import (
	"os"
	"path/filepath"
)

// DiscoverConfigPaths returns the MCP config files that exist on disk, ordered
// from lowest to highest precedence. Later paths override earlier ones on
// server-name collision (see LoadMerged). Non-existent candidates, and any
// that is not a regular file, are skipped, so an empty slice means no MCP
// config anywhere.
func DiscoverConfigPaths(cwd, home string) []string {
	candidates := append(GlobalConfigPaths(home),
		filepath.Join(cwd, ".mcp.json"),
		filepath.Join(cwd, ".celeste", "mcp.json"),
	)

	// Only regular files (a link is followed): a FIFO or a link to a
	// device would block or never end when read (Aikido 806869859).
	var found []string
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			found = append(found, p)
		}
	}
	return found
}

// GlobalConfigPaths returns the home-level MCP config candidates, in the
// precedence order DiscoverConfigPaths uses, whether or not they exist. Any
// other path DiscoverConfigPaths returns came from the workspace.
func GlobalConfigPaths(home string) []string {
	return []string{
		filepath.Join(home, ".celeste", "mcp.json"),
		filepath.Join(home, ".claude", "mcp.json"),
		filepath.Join(home, ".cursor", "mcp.json"),
	}
}

// SplitGlobal splits paths (as DiscoverConfigPaths returns them) into the
// home-level configs every mode loads and the workspace configs only the
// interactive chat loads, each in its original order. With no home every
// path is a workspace one.
func SplitGlobal(paths []string, home string) (global, workspace []string) {
	for _, p := range paths {
		if IsGlobalConfig(home, p) {
			global = append(global, p)
		} else {
			workspace = append(workspace, p)
		}
	}
	return global, workspace
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

// LoadMergedLenient is LoadMerged, except that a workspace config (a path
// not among home's GlobalConfigPaths) that fails to load or parse is
// skipped, its error returned in skipped, instead of failing the whole
// load: a broken repository .mcp.json must not turn off the user's own
// servers (Aikido 806869709). A home config that fails still fails it.
func LoadMergedLenient(paths []string, home string) (cfg *MCPConfig, skipped []error, err error) {
	merged := &MCPConfig{Servers: make(map[string]ServerConfig)}
	for _, p := range paths {
		c, err := LoadConfig(p)
		if err != nil {
			if IsGlobalConfig(home, p) {
				return nil, skipped, err
			}
			skipped = append(skipped, err)
			continue
		}
		for name, sc := range c.Servers {
			sc.Origin = p
			merged.Servers[name] = sc
		}
	}
	return merged, skipped, nil
}
