package loop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

// goWorkspace is a tiny Go module, so the code graph has something to index.
func goWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	write(t, filepath.Join(ws, "go.mod"), "module nestedprobe\n\ngo 1.26\n")
	write(t, filepath.Join(ws, "main.go"), "package main\n\nfunc main() {}\n")
	return ws
}

// untrustedRepoHooks makes every hooks.Load in ws warn "skipping ...": a
// count of that warning is a count of hook loads.
func untrustedRepoHooks(t *testing.T, ws string) {
	t.Helper()
	write(t, filepath.Join(ws, ".celeste", "hooks.json"), `{"hooks":[{"event":"Stop","command":"x"}]}`)
}

func mustNested(t *testing.T, parent Nester, opts NestedOptions) *Env {
	t.Helper()
	child, err := parent.Nested(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(child.Close)
	return child
}

func searchCode(e *Env, query string) (tools.ToolResult, error) {
	tool, ok := e.Registry.Get("code_search")
	if !ok {
		return tools.ToolResult{Error: true, Content: "no code_search tool"}, nil
	}
	return tool.Execute(context.Background(), map[string]any{"query": query, "mode": "keyword"}, nil)
}

func refs(e *Env) int {
	e.shared.mu.Lock()
	defer e.shared.mu.Unlock()
	return e.shared.refs
}

type toolInfo struct{ name string }

func (i toolInfo) ToolName() string { return i.name }
func (i toolInfo) IsReadOnly() bool { return false }

// Same workspace (spelled with a trailing separator): the child reuses the
// parent's hooks, code graph and project context, and loads nothing again.
func TestNestedSharesParentParts(t *testing.T) {
	setupHome(t)
	ws := goWorkspace(t)
	untrustedRepoHooks(t, ws)
	parent, w := mustSetup(t, ModeAgent, ws)

	child := mustNested(t, parent, NestedOptions{Workspace: ws + string(filepath.Separator)})
	if child.Workspace != parent.Workspace {
		t.Fatalf("child workspace %q, want the parent's %q", child.Workspace, parent.Workspace)
	}
	if child.Registry == parent.Registry || child.Checker == parent.Checker || child.Files == parent.Files {
		t.Fatal("registry, checker and file tracker must be the child's own")
	}
	if child.Snapshots != parent.Snapshots {
		t.Fatal("checkpoints go to the parent's session (2.0 F4)")
	}
	if child.Indexer == nil || child.Indexer != parent.Indexer {
		t.Fatal("same workspace: the child must share the parent's code graph")
	}
	if _, ok := child.Registry.Get("code_search"); !ok {
		t.Fatal("the child registry lacks the shared code-graph tools")
	}
	if child.Hooks != parent.Hooks || child.ProjectContext != parent.ProjectContext {
		t.Fatal("same workspace: hooks and project context come from the parent")
	}
	if n := strings.Count(w.all(), "skipping"); n != 1 {
		t.Fatalf("hooks were loaded %d times, want once (by the parent):\n%s", n, w.all())
	}
}

// An isolated worktree is a different workspace: hooks, project context and
// the code graph are the child's own, and closing the child leaves the
// parent's code graph open.
func TestNestedOwnWorkspaceRebuildsBoundParts(t *testing.T) {
	setupHome(t)
	parent, _ := mustSetup(t, ModeAgent, goWorkspace(t))
	other := goWorkspace(t)

	child, err := parent.Nested(NestedOptions{Workspace: other})
	if err != nil {
		t.Fatal(err)
	}
	if child.Workspace != filepath.Clean(other) {
		t.Fatalf("child workspace %q, want %q", child.Workspace, other)
	}
	if child.Indexer == nil || child.Indexer == parent.Indexer {
		t.Fatal("a different workspace needs its own code graph")
	}
	child.Close()
	if res, err := searchCode(parent, "main"); err != nil || res.Error {
		t.Fatalf("closing a worktree child broke the parent's code graph: %v %s", err, res.Content)
	}
}

// Review Focus 2: each child reloads permissions.json. A trusted parent
// doesn't make children trusted, a trusted child doesn't change its parent,
// a rule added after the parent was built reaches the next child, and one
// child's "always allow" never reaches a sibling.
func TestNestedPermissionsAreTheChildsOwn(t *testing.T) {
	home := setupHome(t)
	parent, _ := mustSetup(t, ModeAgent, t.TempDir())
	parent.Trust()
	c := mustNested(t, parent, NestedOptions{})
	if c.Checker.Mode() == permissions.ModeTrust {
		t.Fatal("a child inherited its parent's Trust(); it must start from permissions.json")
	}
	c.Trust()
	other := mustNested(t, parent, NestedOptions{})
	if other.Checker.Mode() == permissions.ModeTrust {
		t.Fatal("a child's Trust() reached its sibling")
	}

	write(t, filepath.Join(home, ".celeste", "permissions.json"), `{"mode":"default","always_deny":[{"tool_pattern":"write_file"}]}`)
	a := mustNested(t, parent, NestedOptions{})
	b := mustNested(t, parent, NestedOptions{})
	if got := a.Checker.Check(toolInfo{name: "write_file"}, map[string]any{}); got.Decision != permissions.Deny {
		t.Fatalf("a rule added after the parent was built did not reach a new child (decision %v)", got.Decision)
	}
	if err := a.Checker.AddPersistentAllow(permissions.Rule{ToolPattern: "bash"}); err != nil {
		t.Fatal(err)
	}
	if got := b.Checker.Check(toolInfo{name: "bash"}, map[string]any{}); got.Decision == permissions.Allow {
		t.Fatal("child a's always-allow leaked into child b")
	}
}

// No recursion: a child never gets spawn_agent, even if the parent's
// registry has one.
func TestNestedHasNoSpawnAgent(t *testing.T) {
	setupHome(t)
	parent, _ := mustSetup(t, ModeAgent, t.TempDir())
	parent.Registry.Register(&fakeTool{name: "spawn_agent"})
	if _, ok := mustNested(t, parent, NestedOptions{}).Registry.Get("spawn_agent"); ok {
		t.Fatal("a nested registry must not contain spawn_agent")
	}
}

// Review Focus 1: the shared parts stay open while a child runs after its
// parent closed, a closed parent can't nest, and the last Close frees them.
func TestNestedOutlivesParentClose(t *testing.T) {
	setupHome(t)
	parent, err := Setup(ModeAgent, testCfg(), goWorkspace(t), SetupOptions{Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	child, err := parent.Nested(NestedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parent.Close()
	if _, err := parent.Nested(NestedOptions{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Nested on a closed parent: err = %v, want a 'closed' error", err)
	}
	if _, err := child.Nested(NestedOptions{}); err == nil {
		t.Fatal("a child must not nest further")
	}
	if res, err := searchCode(child, "main"); err != nil || res.Error {
		t.Fatalf("the child's shared code graph closed with its parent: %v %s", err, res.Content)
	}
	child.Close()
	if n := refs(parent); n != 0 {
		t.Fatalf("refs = %d after every Env closed, want 0", n)
	}
}

// Siblings query one shared code graph at the same time (-race).
func TestNestedChildrenQueryCodeGraphConcurrently(t *testing.T) {
	setupHome(t)
	parent, _ := mustSetup(t, ModeAgent, goWorkspace(t))
	a := mustNested(t, parent, NestedOptions{})
	b := mustNested(t, parent, NestedOptions{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		for _, e := range []*Env{a, b} {
			wg.Add(1)
			go func(e *Env) {
				defer wg.Done()
				if res, err := searchCode(e, "main"); err != nil || res.Error {
					t.Errorf("code_search: %v %s", err, res.Content)
				}
			}(e)
		}
	}
	wg.Wait()
}

// A same-workspace child brings the shared code graph up to date with files
// changed since the parent's Setup (Review Focus 4).
func TestNestedRefreshesTheSharedCodeGraph(t *testing.T) {
	setupHome(t)
	ws := goWorkspace(t)
	parent, _ := mustSetup(t, ModeAgent, ws)
	write(t, filepath.Join(ws, "later.go"), "package main\n\nfunc probeAddedLater() {}\n")
	child := mustNested(t, parent, NestedOptions{})
	res, err := searchCode(child, "probeAddedLater")
	// "No symbols found matching 'probeAddedLater'" also names it: look for
	// the hit itself.
	if err != nil || res.Error || !strings.Contains(res.Content, "Found 1 symbols") || !strings.Contains(res.Content, "later.go") {
		t.Fatalf("the child's code graph lacks a function added after Setup: %v %s", err, res.Content)
	}
}

// A2: a child's own setup warnings and timing notices go to its sinks, not
// the parent's, and a refresh that runs long is bounded.
func TestNestedRoutesItsOwnWarningsAndNotices(t *testing.T) {
	home := setupHome(t)
	origTimeout, origUpdate := nestedCodeGraphTimeout, updateCodeGraph
	t.Cleanup(func() { nestedCodeGraphTimeout, updateCodeGraph = origTimeout, origUpdate })
	var calls atomic.Int32
	updateCodeGraph = func(ctx context.Context, idx *codegraph.Indexer) error {
		if calls.Add(1) == 1 {
			// The parent's Setup update: returns at once. Running it for
			// real let a slow Windows runner hit its 10s timeout, and the
			// parent's own "timed out" warning failed the routing check.
			return nil
		}
		<-ctx.Done() // a child's refresh only stops when the family closes
		return ctx.Err()
	}
	nestedCodeGraphTimeout = 50 * time.Millisecond
	parent, parentWarns := mustSetup(t, ModeAgent, goWorkspace(t))
	write(t, filepath.Join(home, ".celeste", "permissions.json"), "{not json")

	warns, notices := &warnings{}, &warnings{}
	start := time.Now()
	mustNested(t, parent, NestedOptions{Warn: warns.add, Notice: notices.add})
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("Nested took %s; the code-graph refresh must be bounded", d)
	}
	if !strings.Contains(warns.all(), "permissions config") {
		t.Fatalf("child warnings = %q, want its invalid permissions.json reported", warns.all())
	}
	if !strings.Contains(notices.all(), "code graph update timed out") {
		t.Fatalf("child notices = %q, want the refresh timeout", notices.all())
	}
	if p := parentWarns.all(); strings.Contains(p, "permissions config") || strings.Contains(p, "timed out") {
		t.Fatalf("a child's warning reached the parent's sink:\n%s", p)
	}
}

// stubMCPConfig is a global MCP config whose one server is this test binary
// (serveStubMCP).
func stubMCPConfig(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"probe": map[string]any{"enabled": true, "command": exe, "env": map[string]string{mcpStubEnv: "1"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// MCP servers start once for a parent and all its children: each child's
// registry holds the parent's own *MCPTool (one client), in the same
// workspace and in another one.
func TestNestedSharesMCPClients(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "mcp.json"), stubMCPConfig(t))
	parent, w := mustSetup(t, ModeAgent, t.TempDir())
	name := mcp.ToolName("probe", "echo")
	want, ok := parent.Registry.Get(name)
	if !ok {
		t.Fatalf("the parent did not start the stub MCP server:\n%s", w.all())
	}
	if _, isMCP := want.(*mcp.MCPTool); !isMCP {
		t.Fatalf("%s is a %T, want *mcp.MCPTool", name, want)
	}
	for _, ws := range []string{"", t.TempDir()} {
		got, ok := mustNested(t, parent, NestedOptions{Workspace: ws}).Registry.Get(name)
		if !ok || got != want {
			t.Fatalf("workspace %q: child has %v (ok=%v), want the parent's own MCP tool", ws, got, ok)
		}
	}
}

// Parent rebuilds its Env when a config file Setup bakes in changes: the next
// child sees the new permissions.json, a child of the old Env keeps working
// until it closes, and a closed Parent refuses to nest.
func TestParentRebuildsWhenConfigChanges(t *testing.T) {
	home := setupHome(t)
	p := NewParent(testCfg(), goWorkspace(t), SetupOptions{Warn: func(string) {}})
	t.Cleanup(p.Close)
	old := mustNested(t, p, NestedOptions{})
	first := p.env
	if same := mustNested(t, p, NestedOptions{}); p.env != first || same.Indexer != old.Indexer {
		t.Fatal("an unchanged configuration rebuilt the parent Env")
	}

	write(t, filepath.Join(home, ".celeste", "permissions.json"), `{"mode":"default","always_deny":[{"tool_pattern":"write_file"}]}`)
	fresh := mustNested(t, p, NestedOptions{})
	if p.env == first {
		t.Fatal("a permissions.json change did not rebuild the parent Env")
	}
	if got := fresh.Checker.Check(toolInfo{name: "write_file"}, map[string]any{}); got.Decision != permissions.Deny {
		t.Fatalf("the new rule did not reach the next child (decision %v)", got.Decision)
	}
	if res, err := searchCode(old, "main"); err != nil || res.Error {
		t.Fatalf("the rebuild closed a running child's code graph: %v %s", err, res.Content)
	}
	old.Close()
	if n := refs(first); n != 1 {
		t.Fatalf("the replaced Env has %d references with one child left, want 1", n)
	}
	p.Close()
	if _, err := p.Nested(NestedOptions{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Nested on a closed Parent: err = %v, want a 'closed' error", err)
	}
}

// refreshIndex serializes code-graph updates: concurrent same-workspace
// refreshes never run updateCodeGraph at the same time (indexSem in
// refreshIndex, nested.go). This must fail if that semaphore is removed.
func TestNestedRefreshIndexSerializesUpdates(t *testing.T) {
	setupHome(t)
	parent, _ := mustSetup(t, ModeAgent, goWorkspace(t))

	origUpdate := updateCodeGraph
	t.Cleanup(func() { updateCodeGraph = origUpdate })
	var current, max int32
	updateCodeGraph = func(ctx context.Context, idx *codegraph.Indexer) error {
		n := atomic.AddInt32(&current, 1)
		for {
			old := atomic.LoadInt32(&max)
			if n <= old || atomic.CompareAndSwapInt32(&max, old, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond) // hold the lock so racing callers overlap if unserialized
		atomic.AddInt32(&current, -1)
		return nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parent.refreshIndex()
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&max); got != 1 {
		t.Fatalf("max concurrent updateCodeGraph calls = %d, want 1 (refreshIndex must serialize via indexSem)", got)
	}
}

// Worktree hook rebuild (nested.go's `c.setupHooks(c.home)` for a different
// workspace): an other-workspace child rebuilds its own hooks instead of
// sharing the parent's, so an untrusted repo hooks.json there warns the
// child, not the parent, and the child's Hooks differ from the parent's.
// This must fail if that setupHooks call is replaced with `c.Hooks = e.Hooks`.
func TestNestedOtherWorkspaceRebuildsHooksAndWarnsTheChild(t *testing.T) {
	setupHome(t)
	parent, parentWarns := mustSetup(t, ModeAgent, goWorkspace(t))
	other := goWorkspace(t)
	untrustedRepoHooks(t, other)

	childWarns := &warnings{}
	child := mustNested(t, parent, NestedOptions{Workspace: other, Warn: childWarns.add})

	if child.Hooks == parent.Hooks {
		t.Fatal("an other-workspace child must rebuild its own hooks, not share the parent's")
	}
	if n := strings.Count(childWarns.all(), "skipping"); n != 1 {
		t.Fatalf("child warnings = %q, want the other workspace's untrusted hooks.json warning once", childWarns.all())
	}
	if strings.Contains(parentWarns.all(), "skipping") {
		t.Fatalf("the other workspace's hook warning reached the parent's sink:\n%s", parentWarns.all())
	}
}

// A child keeps using its shared MCP tool (the Windows-safe test-binary
// stub) after its parent has closed.
func TestNestedChildRunsMCPToolAfterParentClose(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "mcp.json"), stubMCPConfig(t))
	parent, w := mustSetup(t, ModeAgent, t.TempDir())
	name := mcp.ToolName("probe", "echo")
	if _, ok := parent.Registry.Get(name); !ok {
		t.Fatalf("the parent did not start the stub MCP server:\n%s", w.all())
	}
	child, err := parent.Nested(NestedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parent.Close()

	tool, ok := child.Registry.Get(name)
	if !ok {
		t.Fatal("the child lost its MCP tool once the parent closed")
	}
	res, err := tool.Execute(context.Background(), map[string]any{}, nil)
	if err != nil || res.Error {
		t.Fatalf("MCP tool call after parent close: err=%v res=%+v", err, res)
	}
	child.Close()
}

// Parent reads ConfigStamp under its lock: a caller that waited for the lock
// while a config file changed rebuilds, instead of comparing a stamp read
// before the change and nesting under the stale Env.
func TestParentReadsTheStampUnderItsLock(t *testing.T) {
	home := setupHome(t)
	p := NewParent(testCfg(), goWorkspace(t), SetupOptions{Warn: func(string) {}})
	t.Cleanup(p.Close)
	mustNested(t, p, NestedOptions{})
	first := p.env

	p.mu.Lock() // another caller is nesting
	done := make(chan *Env, 1)
	go func() {
		c, err := p.Nested(NestedOptions{})
		if err != nil {
			t.Error(err)
		}
		done <- c
	}()
	time.Sleep(100 * time.Millisecond) // let the waiting caller reach the lock
	write(t, filepath.Join(home, ".celeste", "permissions.json"), `{"mode":"default","always_deny":[{"tool_pattern":"write_file"}]}`)
	p.mu.Unlock()
	if c := <-done; c != nil {
		c.Close()
	}
	if p.env == first {
		t.Fatal("a config change made while the caller waited for the lock did not rebuild the Env")
	}
}

// A child that arrives while Setup's timed-out update still runs waits for
// it (within nestedCodeGraphTimeout) and then brings the graph up to date
// itself. It used to skip the refresh when the update held the lock, and
// nothing retried it, so the child saw a stale code graph.
func TestNestedRefreshWaitsForAnInFlightUpdate(t *testing.T) {
	setupHome(t)
	ws := goWorkspace(t)
	origUpdate, origSetupTimeout := updateCodeGraph, codeGraphTimeout
	t.Cleanup(func() { updateCodeGraph, codeGraphTimeout = origUpdate, origSetupTimeout })
	indexed, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	updateCodeGraph = func(ctx context.Context, idx *codegraph.Indexer) error {
		if calls.Add(1) > 1 {
			return origUpdate(ctx, idx)
		}
		// Setup's update: index, then keep running past Setup's wait.
		err := origUpdate(ctx, idx)
		close(indexed)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return err
	}
	codeGraphTimeout = 20 * time.Millisecond
	parent, _ := mustSetup(t, ModeAgent, ws)
	<-indexed
	write(t, filepath.Join(ws, "later.go"), "package main\n\nfunc probeAddedLater() {}\n")
	time.AfterFunc(200*time.Millisecond, func() { close(release) })

	child := mustNested(t, parent, NestedOptions{})
	res, err := searchCode(child, "probeAddedLater")
	if err != nil || res.Error || !strings.Contains(res.Content, "Found 1 symbols") {
		t.Fatalf("the child skipped its refresh while Setup's update ran: %v %s", err, res.Content)
	}
}

// The chat's Env is the parent of subagents and /agent (2.0 F2e). A child is
// an agent-mode run: it shares the chat's hooks, code graph and global MCP
// servers, and never a repo's MCP server (non-interactive runs don't run
// them), even though the chat itself started it.
func TestNestedUnderTheChatDropsRepoMCPServers(t *testing.T) {
	home := setupHome(t)
	ws := goWorkspace(t)
	write(t, filepath.Join(home, ".celeste", "mcp.json"), stubMCPConfig(t))
	write(t, filepath.Join(ws, ".mcp.json"), strings.Replace(stubMCPConfig(t), `"probe"`, `"repo"`, 1))
	chat, w := mustSetup(t, ModeChat, ws)
	global, repo := mcp.ToolName("probe", "echo"), mcp.ToolName("repo", "echo")
	for _, name := range []string{global, repo} {
		if _, ok := chat.Registry.Get(name); !ok {
			t.Fatalf("the chat did not start %s:\n%s", name, w.all())
		}
	}
	child := mustNested(t, chat, NestedOptions{})
	if child.Mode != ModeAgent || child.ToolMode != tools.ModeAgent {
		t.Fatalf("child mode = %v/%v, want agent", child.Mode, child.ToolMode)
	}
	if child.Hooks != chat.Hooks || child.Indexer != chat.Indexer {
		t.Fatal("a same-workspace child must share the chat's hooks and code graph")
	}
	want, _ := chat.Registry.Get(global)
	if got, ok := child.Registry.Get(global); !ok || got != want {
		t.Fatalf("child has %v (ok=%v), want the chat's own global MCP tool", got, ok)
	}
	if _, ok := child.Registry.Get(repo); ok {
		t.Fatal("a child of the chat was given the repo's MCP server")
	}
	if _, ok := child.Registry.Get("spawn_agent"); ok {
		t.Fatal("a child of the chat was given spawn_agent")
	}
}
