package builtin

import (
	"strings"
	"testing"
)

// docs/ROADMAP.md is rewritten for 2.0 (#176): what 2.0 shipped, then what
// comes next with where each deferred item came from, and no dates.
func TestRoadmapDescribes20(t *testing.T) {
	doc := repoFile(t, "docs/ROADMAP.md")
	for _, want := range []string{
		"## 2.0 (shipped)",
		"## Next",
		// #176's Docs bullet: compaction, sandboxing and AGENTS.md as delivered.
		"AGENTS.md", "Compaction", "Sandbox", "ACP",
		// Deferred items, each named with its source.
		"persona_lore", "previous_response_id", "ThoughtSignature",
		"Windows sandbox", "LSP diagnostics", "Agent Skills",
		"server compaction", "#199",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/ROADMAP.md does not mention %q", want)
		}
	}
	for _, stale := range []string{"Current Release: v1.16.0", "v2.1.0 Planned", "Last updated: August 2026"} {
		if strings.Contains(doc, stale) {
			t.Errorf("docs/ROADMAP.md still says %q", stale)
		}
	}
	checkNoLocalPaths(t, "docs/ROADMAP.md", doc)
}

// A public doc never names a local machine path.
func checkNoLocalPaths(t *testing.T, file, doc string) {
	t.Helper()
	for _, p := range []string{"/Users/", "/home/", `C:\Users`, ".worktrees", "superpowers/"} {
		if strings.Contains(doc, p) {
			t.Errorf("%s contains a local path fragment %q", file, p)
		}
	}
}
