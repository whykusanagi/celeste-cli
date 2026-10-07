package subagents

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// spawn_agent's workspace argument comes from the model: it may name a
// directory inside the parent's workspace, never one outside it.
func TestSpawnAgentWorkspaceStaysInsideParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "repo")
	sub := filepath.Join(parent, "pkg")
	outside := filepath.Join(root, "elsewhere")
	for _, d := range []string{sub, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A symlink inside the workspace that points out of it.
	if err := os.Symlink(outside, filepath.Join(parent, "link")); err != nil {
		t.Fatal(err)
	}

	m := NewManager(&config.Config{}, parent, false)
	var mu sync.Mutex
	var got []string
	m.execFn = func(_ context.Context, run *SubagentRun, _, ws string, _ TurnCallback, _ int, _ bool) (*SubagentRun, error) {
		mu.Lock()
		got = append(got, ws)
		mu.Unlock()
		m.mu.Lock()
		run.Status = "completed"
		m.mu.Unlock()
		return run, nil
	}
	tool := NewSpawnAgentTool(m)

	for _, ws := range []string{outside, "../elsewhere", filepath.Join(parent, "link"), "link", root} {
		res, err := tool.Execute(context.Background(), map[string]any{"goal": "g", "workspace": ws}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Error {
			t.Errorf("workspace %q outside the parent was accepted: %s", ws, res.Content)
		}
	}
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("a subagent ran in %v", got)
	}
	mu.Unlock()

	for _, ws := range []string{"", "pkg", sub} {
		res, err := tool.Execute(context.Background(), map[string]any{"goal": "g", "workspace": ws}, nil)
		if err != nil || res.Error {
			t.Errorf("workspace %q inside the parent was refused: %v %s", ws, err, res.Content)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("runs = %v, want 3", got)
	}
	if got[1] != sub && got[1] != filepath.Join(parent, "pkg") {
		t.Errorf("relative workspace resolved to %q, want %q", got[1], sub)
	}
}
