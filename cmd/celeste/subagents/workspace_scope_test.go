package subagents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	realSub, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got[1] != realSub {
		t.Errorf("relative workspace resolved to %q, want %q", got[1], sub)
	}
}

// scopeWorkspace returns the path it checked (symlinks resolved), so a link
// swapped in after the check cannot redirect the subagent.
func TestScopeWorkspaceReturnsCheckedPath(t *testing.T) {
	parent := t.TempDir()
	sub := filepath.Join(parent, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sub, filepath.Join(parent, "alias")); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatal(err)
	}
	got, err := scopeWorkspace(parent, "alias")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("scopeWorkspace = %q, want the resolved path %q", got, want)
	}
}

// A workspace checked by spawn_agent is checked again when the subagent is
// built (Aikido, #425): a directory on the checked path that was replaced
// by a symlink out of the parent in between ends the run instead of
// leading its tools outside.
func TestSubagentWorkspaceReplacedAfterTheCheckIsRefused(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "repo")
	sub := filepath.Join(parent, "pkg")
	outside := filepath.Join(root, "elsewhere")
	for _, d := range []string{sub, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scoped, err := scopeWorkspace(parent, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	// Replace the checked directory with a symlink out of the parent.
	if err := os.Rename(sub, sub+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, sub); err != nil {
		t.Fatal(err)
	}

	// An unreachable local endpoint: the test never talks to a provider.
	m := NewManager(&config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1/v1", Model: "fake-model", Timeout: 5}, parent, false)
	run := &SubagentRun{ID: "sub-ws", Status: "running", Workspace: scoped}
	m.mu.Lock()
	m.runs[run.ID] = run
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = m.executeSubagent(ctx, run, "g", scoped, nil, 1, false)
	if err == nil || !strings.Contains(err.Error(), "outside the current workspace") {
		t.Fatalf("executeSubagent = %v; want the replaced workspace refused", err)
	}
	m.mu.Lock()
	status := run.Status
	m.mu.Unlock()
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
}

// The workspace recheck pins the directory it checked: one replaced by a
// symlink out of the parent after the recheck, before the subagent is built
// and run, ends the run instead of redirecting it (Aikido review of #428).
func TestSubagentWorkspaceReplacedAfterTheRecheckIsRefused(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "repo")
	sub := filepath.Join(parent, "pkg")
	outside := filepath.Join(root, "elsewhere")
	for _, d := range []string{sub, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scoped, err := scopeWorkspace(parent, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	swapped := false
	testHookWorkspaceRechecked = func(ws string) {
		if ws != scoped || swapped {
			return
		}
		swapped = true
		if err := os.Rename(sub, sub+".old"); err != nil {
			t.Error(err)
			return
		}
		if err := os.Symlink(outside, sub); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookWorkspaceRechecked = nil })

	m := NewManager(&config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1/v1", Model: "fake-model", Timeout: 5}, parent, false)
	run := &SubagentRun{ID: "sub-ws-pin", Status: "running", Workspace: scoped}
	m.mu.Lock()
	m.runs[run.ID] = run
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = m.executeSubagent(ctx, run, "g", scoped, nil, 1, false)
	if !swapped {
		t.Fatal("hook never ran")
	}
	if err == nil || !strings.Contains(err.Error(), "changed after it was checked") {
		t.Fatalf("executeSubagent = %v; want the swapped workspace refused", err)
	}
	m.mu.Lock()
	status := run.Status
	m.mu.Unlock()
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
}
