package loop

import (
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
)

// The checker knows the run's workspace (Setup, Trust and Nested), so a
// directory-scoped deny also matches the absolute path to the same file.
func TestCheckerKnowsTheWorkspace(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "permissions.json"),
		`{"mode":"default","always_deny":[{"tool_pattern":"write_file(secrets/*)","decision":"deny"}]}`)
	ws := t.TempDir()
	parent, _ := mustSetup(t, ModeAgent, ws)
	deny := func(env *Env, what string) {
		t.Helper()
		in := map[string]any{"path": filepath.Join(env.Workspace, "secrets", "k")}
		if got := env.Checker.Check(toolInfo{name: "write_file"}, in); got.Decision != permissions.Deny {
			t.Errorf("%s: absolute path into secrets/: %v, want Deny", what, got.Decision)
		}
	}
	deny(parent, "Setup")
	parent.Trust()
	deny(parent, "Trust")
	child := mustNested(t, parent, NestedOptions{Workspace: t.TempDir()})
	deny(child, "Nested")
}
