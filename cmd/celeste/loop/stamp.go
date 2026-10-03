package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

// ConfigStamp fingerprints (path, mtime, size) the config files Setup bakes
// into an Env, so an owner that keeps an Env can replace it as soon as one
// changes: a new deny rule, hook, trust approval, skill, MCP server,
// grimoire or AGENTS.md / CLAUDE.md edit applies to the next run. A
// directory's mtime covers files added to or removed from it; an edit inside
// one is not seen. MCP chat's Env cache and Parent use it.
func ConfigStamp(ws string) string {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, ".celeste", "permissions.json"),
		filepath.Join(home, ".celeste", "hooks.json"),
		hooks.TrustPath(home),
		filepath.Join(home, ".celeste", "grimoire.md"),
		filepath.Join(home, ".celeste", "skills"),
		filepath.Join(ws, ".grimoire"),
		filepath.Join(ws, ".grimoire.local"),
		filepath.Join(ws, ".celeste", "grimoire"),
		filepath.Join(ws, ".celeste", "hooks.json"),
		filepath.Join(ws, ".celeste", "config.json"), // its "sandbox" settings (W4)
		// Not the memory index: save_memory writes it on every call, and a
		// full rebuild per save costs more than a summary a few minutes
		// stale (the memory tools read live).
	}
	paths = append(paths, mcp.GlobalConfigPaths(home)...)
	// AGENTS.md / CLAUDE.md from the git root down to the workspace (W4).
	paths = append(paths, grimoire.ContextFilePaths(ws)...)
	var b strings.Builder
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%s|%d|%d\n", p, fi.ModTime().UnixNano(), fi.Size())
		} else {
			fmt.Fprintf(&b, "%s|-\n", p)
		}
	}
	return b.String()
}
