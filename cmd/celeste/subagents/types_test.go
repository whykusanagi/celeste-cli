package subagents

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
)

// stubTool is a minimal tools.Tool for tool-set tests.
type stubTool struct {
	name     string
	readOnly bool
}

func (s stubTool) Name() string                               { return s.name }
func (s stubTool) Description() string                        { return "" }
func (s stubTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) IsConcurrencySafe(map[string]any) bool      { return true }
func (s stubTool) IsReadOnly() bool                           { return s.readOnly }
func (s stubTool) ValidateInput(map[string]any) error         { return nil }
func (s stubTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (s stubTool) Execute(context.Context, map[string]any, chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func toolNames(r *tools.Registry) map[string]bool {
	out := map[string]bool{}
	for _, d := range r.GetAll() {
		out[d.Name()] = true
	}
	return out
}

// Review Focus 1.
func TestExploreSubagentToolSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	reg := tools.NewRegistry()
	builtin.RegisterAll(reg, t.TempDir(), nil, nil, nil)
	reg.Register(NewSubmitResultTool(&resultHolder{}))
	reg.Register(stubTool{name: "spawn_agent"})
	reg.Register(stubTool{name: "post_message"})
	reg.Register(stubTool{name: "mcp_repo_lookup"}) // an MCP tool: never read-only
	p := profileFor(TypeExplore, &config.Config{Model: "big", SmallModel: "small"})
	reg.Retain(p.Allow)
	got := toolNames(reg)
	for _, want := range []string{"read_file", "list_files", "search", "git_status", "submit_result"} {
		if !got[want] {
			t.Errorf("explore lacks %s", want)
		}
	}
	for _, banned := range []string{"write_file", "patch_file", "splice_file", "bash", "spawn_agent", "post_message", "save_memory", "todo", "mcp_repo_lookup"} {
		if got[banned] {
			t.Errorf("explore must not have %s", banned)
		}
	}
	if !p.SkipPersona || p.Model != "small" {
		t.Fatalf("explore profile = %+v", p)
	}
}

func TestReviewToolSet(t *testing.T) {
	reg := tools.NewRegistry()
	for _, n := range []string{"read_file", "list_files", "search", "git_status", "git_log", "code_search", "code_graph", "submit_result", "write_file", "bash", "web_fetch"} {
		reg.Register(stubTool{name: n, readOnly: n != "write_file" && n != "bash"})
	}
	reg.Register(stubTool{name: "code_deploy"})    // a custom or MCP tool named code_*: not read-only
	reg.Register(builtin.NewCodeSnapshotTool(nil)) // the built-in, not read-only, still in
	reg.Retain(profileFor(TypeReview, &config.Config{Model: "m"}).Allow)
	got := toolNames(reg)
	if got["code_deploy"] {
		t.Error("review must not take a mutating tool just because it is named code_*")
	}
	for _, want := range []string{"read_file", "list_files", "search", "git_status", "git_log", "code_search", "code_graph", "code_snapshot", "submit_result"} {
		if !got[want] {
			t.Errorf("review lacks %s", want)
		}
	}
	for _, banned := range []string{"write_file", "bash", "web_fetch"} {
		if got[banned] {
			t.Errorf("review must not have %s", banned)
		}
	}
}

func TestReviewAndGeneralProfiles(t *testing.T) {
	cfg := &config.Config{Model: "chat", AgentModel: "agent", SmallModel: "small"}
	if p := profileFor(TypeReview, cfg); !p.SkipPersona || p.Model != "agent" {
		t.Fatalf("review = %+v", p)
	}
	if p := profileFor(TypeGeneral, cfg); p.SkipPersona || p.Model != "agent" || p.Allow != nil {
		t.Fatalf("general = %+v", p)
	}
	if typ, err := ParseType(""); err != nil || typ != TypeGeneral {
		t.Fatalf("absent type = %q %v, want general", typ, err)
	}
	_, err := ParseType("wizard")
	if err == nil {
		t.Fatal("unknown type must be an error")
	}
	for _, name := range []string{"explore", "general", "review"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not list %s", err, name)
		}
	}
}

// spawn_agent validates type and refuses a persona on explore/review, and
// passes the type to the run.
func TestSpawnAgentTypeArgument(t *testing.T) {
	m := NewManager(&config.Config{}, t.TempDir(), false)
	var mu sync.Mutex
	var seen []Type
	m.execFn = func(_ context.Context, run *SubagentRun, _, _ string, _ TurnCallback, _ int, _ bool) (*SubagentRun, error) {
		mu.Lock()
		seen = append(seen, run.Type)
		mu.Unlock()
		m.mu.Lock()
		run.Status, run.Result = "completed", "ok"
		m.mu.Unlock()
		return run, nil
	}
	tool := NewSpawnAgentTool(m)
	if err := tool.ValidateInput(map[string]any{"goal": "g", "type": "wizard"}); err == nil || !strings.Contains(err.Error(), "explore") {
		t.Fatalf("unknown type: %v", err)
	}
	if err := tool.ValidateInput(map[string]any{"goal": "g", "type": "explore", "persona": map[string]any{"flirt": 3.0}}); err == nil || !strings.Contains(err.Error(), "without the persona") {
		t.Fatalf("persona on explore: %v", err)
	}
	if err := tool.ValidateInput(map[string]any{"goal": "g", "type": "general", "persona": map[string]any{"flirt": 3.0}}); err != nil {
		t.Fatalf("persona on general: %v", err)
	}
	res, _ := tool.Execute(context.Background(), map[string]any{"goal": "g", "type": "review", "persona": map[string]any{}}, nil)
	if !res.Error {
		t.Fatalf("Execute must refuse a persona on review: %+v", res)
	}
	for _, in := range []map[string]any{{"goal": "a", "type": "explore"}, {"goal": "b"}} {
		if res, _ := tool.Execute(context.Background(), in, nil); res.Error {
			t.Fatalf("spawn %v: %+v", in, res)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != TypeExplore || seen[1] != TypeGeneral {
		t.Fatalf("types reaching the run = %v", seen)
	}
}
