package loop

import (
	"path/filepath"
	"testing"
)

// The stamp covers the files Setup bakes into an Env (moved from the MCP
// chat Env cache, F2b).
func TestConfigStampSeesConfigChanges(t *testing.T) {
	home := setupHome(t)
	ws := t.TempDir()
	base := ConfigStamp(ws)
	for _, p := range []string{
		filepath.Join(home, ".celeste", "permissions.json"),
		filepath.Join(home, ".celeste", "hooks.json"),
		filepath.Join(home, ".celeste", "trusted.json"),
		filepath.Join(home, ".celeste", "mcp.json"),
		filepath.Join(ws, ".grimoire"),
		filepath.Join(ws, ".celeste", "hooks.json"),
	} {
		write(t, p, "{}")
		if ConfigStamp(ws) == base {
			t.Errorf("stamp ignores %s", p)
		}
		base = ConfigStamp(ws)
	}
}
