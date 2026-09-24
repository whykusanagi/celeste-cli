package agent

import (
	"regexp"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
)

// promptToolRef matches the ways buildAgentSystemPrompt names a tool for the
// model to call: "call X", "using X", and the "name" field of the text-format
// <tool_call> example.
var promptToolRef = regexp.MustCompile(`(?:\bcall|\busing)\s+([a-z][a-z0-9_]*)|"name":\s*"([a-z][a-z0-9_]*)"`)

// TestAgentPromptToolsAreRegistered pins every tool the agent system prompt
// tells the model to call to the agent-mode registry. The prompt once named
// dev_* tools that had been renamed, so text-format tool calls failed with
// "tool not found" (#166).
func TestAgentPromptToolsAreRegistered(t *testing.T) {
	registry := tools.NewRegistry()
	builtin.RegisterAll(registry, t.TempDir(), nil, nil, nil)
	registered := map[string]bool{}
	for _, tool := range registry.GetTools(tools.ModeAgent) {
		registered[tool.Name()] = true
	}

	prompt := buildAgentSystemPrompt(Options{
		RequireVerification:  true,
		VerificationCommands: []string{"go test ./..."},
	}, "OS: test")

	if strings.Contains(prompt, "dev_") {
		t.Errorf("agent prompt still references a dev_* tool name")
	}

	var referenced []string
	for _, m := range promptToolRef.FindAllStringSubmatch(prompt, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		referenced = append(referenced, name)
		if !registered[name] {
			t.Errorf("agent prompt tells the model to call %q, which is not registered in agent mode", name)
		}
	}

	// Guard the guard: if the regex stops matching, the loop above passes vacuously.
	for _, want := range []string{"read_file", "write_file", "patch_file", "bash", "list_files", "search"} {
		found := false
		for _, name := range referenced {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected the agent prompt to reference %q; referenced: %v", want, referenced)
		}
	}
}
