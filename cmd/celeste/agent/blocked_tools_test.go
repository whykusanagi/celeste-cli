package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// stubTool is the minimum a tool needs to be registered and permission-checked.
type stubTool struct {
	name     string
	readOnly bool
}

func (s stubTool) Name() string                               { return s.name }
func (s stubTool) Description() string                        { return s.name }
func (s stubTool) IsReadOnly() bool                           { return s.readOnly }
func (s stubTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) IsConcurrencySafe(_ map[string]any) bool    { return true }
func (s stubTool) ValidateInput(_ map[string]any) error       { return nil }
func (s stubTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (s stubTool) Execute(_ context.Context, _ map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func registryWith(toolsIn ...tools.Tool) *tools.Registry {
	r := tools.NewRegistry()
	for _, t := range toolsIn {
		r.RegisterWithModes(t, tools.ModeAgent)
	}
	return r
}

// Under the default policy only read-only tools are granted, so every mutating
// tool lands on Ask. `celeste agent` has no prompt, so those would each be
// denied while the run still reported success — the case this must catch.
func TestBlockedMutatingTools_DefaultPolicyBlocksWrites(t *testing.T) {
	reg := registryWith(
		stubTool{name: "read_file", readOnly: true},
		stubTool{name: "write_file"},
		stubTool{name: "bash"},
	)
	checker := permissions.NewChecker(permissions.DefaultConfig())

	got := blockedMutatingTools(reg, checker)

	want := []string{"bash", "write_file"} // sorted, read_file excluded
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

// Trust mode is what AutoApproveTools switches on. Nothing should be reported
// as blocked, otherwise -auto-approve would refuse to start.
func TestBlockedMutatingTools_TrustModeBlocksNothing(t *testing.T) {
	reg := registryWith(
		stubTool{name: "write_file"},
		stubTool{name: "bash"},
	)
	cfg := permissions.DefaultConfig()
	cfg.Mode = permissions.ModeTrust
	checker := permissions.NewChecker(cfg)

	if got := blockedMutatingTools(reg, checker); len(got) != 0 {
		t.Errorf("trust mode should block nothing, got %v", got)
	}
}

// A tool explicitly granted in always_allow is not blocked. This is why the bug
// stayed hidden for anyone who had approved these once in the TUI.
func TestBlockedMutatingTools_GrantedToolNotBlocked(t *testing.T) {
	reg := registryWith(stubTool{name: "write_file"}, stubTool{name: "bash"})
	cfg := permissions.DefaultConfig()
	cfg.AlwaysAllow = append(cfg.AlwaysAllow, permissions.Rule{
		ToolPattern: "write_file", Decision: permissions.Allow,
	})
	checker := permissions.NewChecker(cfg)

	got := blockedMutatingTools(reg, checker)
	if len(got) != 1 || got[0] != "bash" {
		t.Errorf("only bash should be blocked, got %v", got)
	}
}

// A read-only-only toolset is degraded but not broken, so it must not refuse.
func TestBlockedMutatingTools_ReadOnlyToolsNeverBlock(t *testing.T) {
	reg := registryWith(
		stubTool{name: "read_file", readOnly: true},
		stubTool{name: "list_files", readOnly: true},
	)
	checker := permissions.NewChecker(permissions.DefaultConfig())

	if got := blockedMutatingTools(reg, checker); len(got) != 0 {
		t.Errorf("read-only tools must never block, got %v", got)
	}
}
