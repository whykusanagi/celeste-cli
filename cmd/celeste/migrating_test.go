package main

import (
	"os"
	"strings"
	"testing"
)

// MIGRATING-2.0.md covers every 2.0 breaking change and migration row (W7).
func TestMigratingCoversEveryBreakingChange(t *testing.T) {
	data, err := os.ReadFile("../../MIGRATING-2.0.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, must := range []string{
		"github.com/whykusanagi/celeste-cli/v2/cmd/celeste@latest", // module path
		"runtime_mode", "-mode", "claw_max_tool_iterations", // W6b
		"## Hooks", "hooks.json", "celeste hooks trust", // F0
		"skip_persona_prompt",                                        // W5
		"celeste_essence.json", "persona verify", "official release", // W5-A/#268
		"context_limit",          // W5-B window guard
		".grimoire", "AGENTS.md", // W4a-1
		"Responses",               // W8
		"celeste update",          // W5-D
		"CELESTE_NO_AUTO_UPGRADE", // W5-D
		"patch_file",              // W4b-1
		"## One tool loop",        // F2
		`"trusted": true`,         // W4a-3
		"sandbox.enabled",         // W4c-2
		"/rewind", "/fork",        // W4d-1
		"celeste acp",        // W4f
		"## Images",          // #239
		"permissions.json",   // F2c
		"UserPromptSubmit",   // F2e
		"/plan", "plan.json", // W4e-2
	} {
		if !strings.Contains(doc, must) {
			t.Errorf("MIGRATING-2.0.md does not mention %q", must)
		}
	}
	// The pre-/v2 install path is never spelled out: Part C's
	// TestInstallLinesUseTheModulePath scans this file for it.
	if strings.Contains(doc, "github.com/whykusanagi/celeste-cli/cmd/") {
		t.Error("MIGRATING-2.0.md spells the pre-/v2 package path")
	}
}
