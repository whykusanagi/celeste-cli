package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCarriesContextFilesUnderTheGrimoire(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(ws, ".grimoire"), "## Conventions\n- tabs\n")
	write(t, filepath.Join(ws, "AGENTS.md"), "Run make test before pushing.")
	for _, mode := range []Mode{ModeChat, ModeAgent, ModeMCPChat} {
		env, _ := mustSetup(t, mode, ws)
		pc := env.ProjectContext
		g, a := strings.Index(pc, "tabs"), strings.Index(pc, "Run make test")
		if g < 0 || a < 0 || a < g {
			t.Errorf("mode %v: grimoire then context files expected:\n%s", mode, pc)
		}
		if !strings.Contains(env.GrimoireContext, "Run make test") {
			t.Errorf("mode %v: /grimoire view lacks the context files", mode)
		}
	}
}

// A change to AGENTS.md must rebuild a cached MCP chat Env.
func TestConfigStampSeesContextFiles(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	before := ConfigStamp(ws)
	write(t, filepath.Join(ws, "CLAUDE.md"), "x")
	if ConfigStamp(ws) == before {
		t.Fatal("ConfigStamp ignores CLAUDE.md")
	}
}
