package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

func hooksJSON(t *testing.T, defs ...map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"hooks": defs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hookDef(event, matcher, command string) map[string]any {
	d := map[string]any{"event": event, "command": command}
	if matcher != "" {
		d["matcher"] = matcher
	}
	return d
}

func systemText(req fakeprovider.Request) string {
	msgs, _ := req.Body["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "system" {
			s, _ := mm["content"].(string)
			return s
		}
	}
	return ""
}

func toolNames(req fakeprovider.Request) []string {
	list, _ := req.Body["tools"].([]any)
	var names []string
	for _, tl := range list {
		fn, _ := tl.(map[string]any)["function"].(map[string]any)
		if n, _ := fn["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	return names
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// mcpChatEnv builds a ModeMCPChat environment in a temp HOME, closed at
// cleanup.
func mcpChatEnv(t *testing.T) (*loop.Env, *config.Config) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	env, err := loop.Setup(loop.ModeMCPChat, cfg, t.TempDir(), loop.SetupOptions{Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	return env, cfg
}

// chatServer is a Server for direct runChatMode calls, closed at cleanup
// (after contractCfg's TempDirs were registered, so it closes first).
func chatServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	srv := New(cfg)
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// countBuilds counts srv's chat Env builds.
func countBuilds(srv *Server) *atomic.Int32 {
	var n atomic.Int32
	inner := srv.chatEnvs.build
	srv.chatEnvs.build = func(c *config.Config, ws string, o loop.SetupOptions) (*loop.Env, error) {
		n.Add(1)
		return inner(c, ws, o)
	}
	return &n
}

// 2.0 F2b intentional change: a failed tool call reaches the model in the
// loop's JSON envelope. The pre-loop server sent a denied call's raw text.
func TestMCPChatToolErrorsUseTheLoopEnvelope(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "b", Name: "bash", Args: `{"command":"sudo ls"}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	cfg, ws := contractCfg(t, fp)
	chatVia(t, cfg, ws, "sudo")
	got := toolContent(fp.Requests()[1], "b")
	var envl struct {
		Error   bool   `json:"error"`
		Tool    string `json:"tool"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(got), &envl); err != nil || !envl.Error || envl.Tool != "bash" || !strings.HasPrefix(envl.Message, "Permission denied") {
		t.Fatalf("tool error not in the loop envelope: %q", got)
	}
}

// 2.0 F2b setup coverage: custom skills, the code-graph tools, memories and
// the code-graph summary now reach the model.
func TestMCPChatSetupCoverage(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	cfg, ws := contractCfg(t, fp)
	writeFile(t, filepath.Join(testHome(t), ".celeste", "skills", "hello.json"),
		`{"name":"hello_skill","description":"x","parameters":{"type":"object"},"command":"echo hi"}`)
	store := memories.NewStore(ws)
	idx, _ := memories.LoadIndex(filepath.Join(store.BaseDir(), "MEMORY.md"))
	_ = idx.Add(memories.IndexEntry{Name: "coverage-memory", File: "c.md", Description: "probe"})
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}
	if r := chatVia(t, cfg, ws, "hi"); r.IsError || r.Text != "ok" {
		t.Fatalf("got %+v", r)
	}
	req := fp.Requests()[0]
	names, sys := toolNames(req), systemText(req)
	for row, ok := range map[string]bool{
		"mcp_server.custom_skills":      hasName(names, "hello_skill"),
		"mcp_server.code_graph_tools":   hasName(names, "code_search"),
		"mcp_server.memories":           strings.Contains(sys, "coverage-memory"),
		"mcp_server.code_graph_summary": strings.Contains(sys, "# Code Graph"),
		"mcp_server.builtins":           hasName(names, "write_file") && hasName(names, "save_memory"),
	} {
		if !ok {
			t.Errorf("%s not wired", row)
		}
	}
}

// Tool hooks run in the registry (F0) for MCP chat too.
func TestMCPChatRunsGlobalToolHooks(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"NOTES.md","content":"hello"}`}}},
		fakeprovider.Turn{Text: "blocked"},
	)
	cfg, ws := contractCfg(t, fp)
	writeFile(t, filepath.Join(testHome(t), ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("PreToolUse", "write_file", hooktest.Command(t, "deny", "frozen in test"))))
	chatVia(t, cfg, ws, "write")
	if got := toolContent(fp.Requests()[1], "w"); !strings.Contains(got, "Blocked by pre-tool hook: frozen in test") {
		t.Fatalf("hook did not block: %q", got)
	}
	if _, err := os.Stat(filepath.Join(ws, "NOTES.md")); !os.IsNotExist(err) {
		t.Fatalf("blocked write happened: %v", err)
	}
}

// Review focus 4: a repo hook in the workspace is untrusted, so MCP chat
// (non-interactive) skips it and the warning comes back to the caller.
func TestMCPChatSkipsUntrustedRepoHooks(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"NOTES.md","content":"hello"}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	cfg, ws := contractCfg(t, fp)
	marker := filepath.Join(t.TempDir(), "ran")
	writeFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("PreToolUse", "write_file", hooktest.Command(t, "record", marker))))
	r := chatVia(t, cfg, ws, "write")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("untrusted repo hook ran: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "NOTES.md")); err != nil || string(b) != "hello" {
		t.Fatalf("write_file did not run: %v %q", err, b)
	}
	if !strings.HasPrefix(r.Text, "done\n\n## Warnings\n\n- ") || !strings.Contains(r.Text, "hooks: skipping 1 hook(s)") {
		t.Fatalf("warning not returned after the reply:\n%s", r.Text)
	}
}

// Setup warnings come back on the call that built the Env, and only there.
func TestMCPChatSetupWarningsComeBackOnce(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
	cfg, ws := contractCfg(t, fp)
	repoCfg := filepath.Join(ws, ".mcp.json")
	writeFile(t, repoCfg, `{"mcpServers":{"probe":{"enabled":true,"command":"true"}}}`)
	srv := chatServer(t, cfg)
	first, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, "hi", ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"one", "## Warnings", strconv.Quote(repoCfg), "non-interactive"} {
		if !strings.Contains(first[0].Text, want) {
			t.Errorf("first result lacks %q:\n%s", want, first[0].Text)
		}
	}
	second, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, "hi", ws)
	if err != nil || second[0].Text != "two" {
		t.Fatalf("second call: %v %q", err, second)
	}
}

// Calls on one workspace reuse its Env; each call has its own history.
func TestMCPChatReusesTheEnvAcrossCalls(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
	cfg, ws := contractCfg(t, fp)
	srv := chatServer(t, cfg)
	builds := countBuilds(srv)
	for _, p := range []string{"first", "second"} {
		if _, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, p, ws); err != nil {
			t.Fatal(err)
		}
	}
	if builds.Load() != 1 {
		t.Fatalf("builds = %d, want 1", builds.Load())
	}
	msgs, _ := fp.Requests()[1].Body["messages"].([]any)
	if len(msgs) != 2 { // system + this call's prompt: no history carried over
		t.Fatalf("second call carried history: %d messages", len(msgs))
	}
}

// Review focus 2: a deny rule added between calls applies to the next call.
func TestMCPChatPicksUpANewDenyRuleOnTheNextCall(t *testing.T) {
	write := func(id, path string) fakeprovider.Turn {
		return fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: id, Name: "write_file", Args: `{"path":"` + path + `","content":"x"}`}}}
	}
	fp := fakeprovider.NewOpenAI(t, write("a", "a.txt"), fakeprovider.Turn{Text: "one"}, write("b", "b.txt"), fakeprovider.Turn{Text: "two"})
	cfg, ws := contractCfg(t, fp)
	srv := chatServer(t, cfg)
	if _, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, "write a", ws); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(testHome(t), ".celeste", "permissions.json"),
		`{"mode":"default","always_deny":[{"tool_pattern":"write_file","decision":"deny"}]}`)
	if _, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, "write b", ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "a.txt")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("the new deny rule was ignored: %v", err)
	}
}

// Setup's FileTracker (2.0 F2b intentional change): within a call, a file
// changed after the model read it is not patched blind. A trusted global
// PreToolUse hook rewrites a.txt just before patch_file runs, which is a
// change Celeste didn't make.
func TestMCPChatRefusesStaleEdits(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p", Name: "patch_file", Args: `{"path":"a.txt","old_string":"one","new_string":"two","replace_all":false}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	cfg, ws := contractCfg(t, fp)
	target := filepath.Join(ws, "a.txt")
	writeFile(t, target, "one\n")
	writeFile(t, filepath.Join(testHome(t), ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("PreToolUse", "patch_file", hooktest.Command(t, "record", target))))
	chatVia(t, cfg, ws, "patch a.txt")
	reqs := fp.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	if got := toolContent(reqs[2], "p"); !strings.Contains(got, "modified externally since you last read it") {
		t.Fatalf("stale patch not refused: %q", got)
	}
}

// Staleness is per call: a file the user edits between two calls is not
// stale in the second call. The tracker resets per call, so (2.0 W4
// must-read-before-edit) the second call reads the file before patching it;
// a patch without that read is refused with the read hint.
func TestMCPChatStalenessIsPerCall(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "read"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p0", Name: "patch_file", Args: `{"path":"a.txt","old_string":"one","new_string":"two","replace_all":false}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r2", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p", Name: "patch_file", Args: `{"path":"a.txt","old_string":"one","new_string":"two","replace_all":false}`}}},
		fakeprovider.Turn{Text: "patched"},
	)
	cfg, ws := contractCfg(t, fp)
	target := filepath.Join(ws, "a.txt")
	writeFile(t, target, "one\n")
	srv := chatServer(t, cfg)
	if _, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, "read", ws); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(target, later, later); err != nil { // the user's edit
		t.Fatal(err)
	}
	if _, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, "patch", ws); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "two\n" {
		t.Fatalf("second call's patch refused: %q (%q)", b, toolContent(fp.Requests()[5], "p"))
	}
	if got := toolContent(fp.Requests()[3], "p0"); !strings.Contains(got, "read_file a.txt first") {
		t.Fatalf("an unread patch in a new call must ask for a read: %q", got)
	}
}

// A call cancelled before it starts spends no provider request and builds
// no Env.
func TestMCPChatCancelledCallIsAChatError(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "never sent"})
	cfg, ws := contractCfg(t, fp)
	srv := chatServer(t, cfg)
	builds := countBuilds(srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := srv.runChatMode(ctx, cfg.CelesteConfig, "hi", ws)
	if err == nil || !strings.HasPrefix(err.Error(), "chat error: ") || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if n := len(fp.Requests()); n != 0 || builds.Load() != 0 {
		t.Fatalf("requests = %d, builds = %d", n, builds.Load())
	}
}

// Review focus 3: a call cancelled while a tool (here its 10 s PreToolUse
// hook) runs returns promptly, and the Env serves the next call.
func TestMCPChatCancelDuringAToolReturnsPromptly(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"a.txt","content":"x"}`}}},
		fakeprovider.Turn{Text: "after"},
	)
	cfg, ws := contractCfg(t, fp)
	writeFile(t, filepath.Join(testHome(t), ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("PreToolUse", "write_file", hooktest.Command(t, "sleep"))))
	srv := chatServer(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	_, err := srv.runChatMode(ctx, cfg.CelesteConfig, "write", ws)
	if err == nil || !strings.HasPrefix(err.Error(), "chat error: ") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 9*time.Second {
		t.Fatalf("cancel took %s; the call waited for the tool", d)
	}
	if out, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, "again", ws); err != nil || !strings.HasPrefix(out[0].Text, "after") {
		t.Fatalf("the Env did not serve the next call: %v %+v", err, out)
	}
}

// Review focus 1: parallel calls on one workspace share one Env (one Setup)
// and both finish. Run under -race.
func TestMCPChatConcurrentCallsShareOneEnv(t *testing.T) {
	fpA := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "a", Name: "write_file", Args: `{"path":"a.txt","content":"A"}`}}},
		fakeprovider.Turn{Text: "wrote a"},
	)
	fpB := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "b", Name: "write_file", Args: `{"path":"b.txt","content":"B"}`}}},
		fakeprovider.Turn{Text: "wrote b"},
	)
	cfg, ws := contractCfg(t, fpA)
	srv := chatServer(t, cfg)
	builds := countBuilds(srv)
	ccA := *cfg.CelesteConfig
	ccB := ccA
	ccB.BaseURL = fpB.BaseURL()
	ccs := []*config.Config{&ccA, &ccB}
	outs := make([][]ContentBlock, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range ccs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i], errs[i] = srv.runChatMode(context.Background(), ccs[i], "write", ws)
		}()
	}
	wg.Wait()
	for i, want := range []string{"wrote a", "wrote b"} {
		if errs[i] != nil || len(outs[i]) != 1 || !strings.HasPrefix(outs[i][0].Text, want) {
			t.Errorf("call %d: %v %+v", i, errs[i], outs[i])
		}
	}
	if builds.Load() != 1 {
		t.Errorf("builds = %d, want 1", builds.Load())
	}
	for name, want := range map[string]string{"a.txt": "A", "b.txt": "B"} {
		if b, err := os.ReadFile(filepath.Join(ws, name)); err != nil || string(b) != want {
			t.Errorf("%s: %v %q", name, err, b)
		}
	}
}

// A hook warning belongs to the call whose tool raised it. Call B is held
// mid-run (after its tool ran) while call A's PreToolUse hook fails; B's
// result must not carry A's warning, which a caller may paste into a file.
func TestMCPChatHookWarningsStayOnTheirCall(t *testing.T) {
	fpA := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "warm"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "a", Name: "write_file", Args: `{"path":"a.txt","content":"A"}`}}},
		fakeprovider.Turn{Text: "wrote a"},
	)
	fpB := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "b", Name: "list_files", Args: `{"path":"."}`}}},
		fakeprovider.Turn{Text: "listed"},
	)
	// B's second model request waits here until A has finished.
	target, err := url.Parse(fpB.BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: target.Scheme, Host: target.Host})
	proxy.FlushInterval = -1
	held, resume := make(chan struct{}), make(chan struct{})
	var n atomic.Int32
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 2 {
			close(held)
			<-resume
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(gate.Close)

	cfg, ws := contractCfg(t, fpA)
	writeFile(t, filepath.Join(testHome(t), ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("PreToolUse", "write_file", hooktest.Command(t, "exit", "1", "boom"))))
	srv := chatServer(t, cfg)
	ccA := *cfg.CelesteConfig
	if _, err := srv.runChatMode(context.Background(), &ccA, "warm", ws); err != nil {
		t.Fatal(err) // builds the Env, so neither call below gets Setup warnings
	}
	ccB := ccA
	ccB.BaseURL = gate.URL + target.Path
	var outB []ContentBlock
	var errB error
	done := make(chan struct{})
	go func() {
		defer close(done)
		outB, errB = srv.runChatMode(context.Background(), &ccB, "list", ws)
	}()
	select {
	case <-held:
	case <-done:
		t.Fatalf("call B finished before its second request: %v %+v", errB, outB)
	}
	outA, errA := srv.runChatMode(context.Background(), &ccA, "write", ws)
	close(resume)
	<-done
	if errA != nil || len(outA) != 1 || !strings.HasPrefix(outA[0].Text, "wrote a\n\n## Warnings\n\n- ") ||
		!strings.Contains(outA[0].Text, "PreToolUse hook from") {
		t.Fatalf("call A must carry its own hook warning: %v %+v", errA, outA)
	}
	if errB != nil || len(outB) != 1 || outB[0].Text != "listed" {
		t.Fatalf("call B got another call's warning: %v %+v", errB, outB)
	}
}

func TestChatLimitsKeepTheServersGuards(t *testing.T) {
	want := loop.Limits{
		MaxTurns:        25,
		IdenticalCalls:  3,
		NoProgressTurns: 6,
		SpillBytes:      ctxmgr.DefaultMaxToolResultBytes,
		ToolTimeout:     loop.DefaultToolTimeout,
		HookBudget:      loop.DefaultHookBudget,
	}
	if got := chatLimits(); got != want {
		t.Fatalf("chatLimits() = %+v, want %+v", got, want)
	}
}

func TestChatClaimsFollowToolResults(t *testing.T) {
	const audio, spawn = "Audio saved: /tmp/x.mp3", "Subagent spawned (id: task-47)"
	var c chatClaims
	if got := c.strip(audio); got == audio {
		t.Fatal("unbacked audio claim kept")
	}
	c.observe(loop.Event{Kind: loop.EventToolResult, Call: loop.ToolCall{Name: "generate_speech"}, IsError: true})
	if got := c.strip(audio); got == audio {
		t.Fatal("a failed generate_speech must not back the claim")
	}
	c.observe(loop.Event{Kind: loop.EventToolResult, Call: loop.ToolCall{Name: "generate_speech"}})
	if got := c.strip(audio); got != audio {
		t.Fatalf("backed audio claim stripped: %q", got)
	}
	if got := c.strip(spawn); got == spawn {
		t.Fatal("unbacked spawn claim kept")
	}
	c.observe(loop.Event{Kind: loop.EventToolResult, Call: loop.ToolCall{Name: "spawn_agent"}})
	if got := c.strip(spawn); got != spawn {
		t.Fatalf("backed spawn claim stripped: %q", got)
	}
}

// MCP chat is headless: no Gate, so an Ask (a hook-forced one; Trust mode
// asks for nothing else) is denied.
func TestNewChatLoopIsHeadless(t *testing.T) {
	env, cfg := mcpChatEnv(t)
	l := newChatLoop(cfg, newChatClient(cfg, env.Registry, "sys"), env, "sys", "mcp-chat-test")
	if l.Gate != nil {
		t.Error("MCP chat must have no Gate")
	}
	if l.Tools != env.Registry || l.Limits != chatLimits() || l.SessionID != "mcp-chat-test" {
		t.Errorf("loop = %+v", l)
	}
}

// toolTurns builds a history of n read_file calls, oldest first, whose
// results are size bytes each.
func toolTurns(n, size int) []loop.Message {
	msgs := []loop.Message{{Role: "user", Content: "read everything"}}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("c%d", i)
		msgs = append(msgs,
			loop.Message{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}}},
			loop.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", size)},
		)
	}
	return msgs
}

// 2.0 F2b intentional change: about 96k tokens of results against a 64k
// window (48k threshold) get the oldest elided, never removed.
func TestChatCompactorPrunesNearTheWindow(t *testing.T) {
	c := &chatCompactor{window: 64_000, meter: compact.NewMeter(0), store: &compact.Store{Dir: t.TempDir()}}
	in := toolTurns(12, 32_000)
	out, notes, changed := c.Compact(context.Background(), in, nil, false)
	if !changed || len(notes) != 1 || !strings.HasPrefix(notes[0], "context compacted: ") {
		t.Fatalf("changed=%v notes=%q", changed, notes)
	}
	if len(out) != len(in) {
		t.Fatalf("pruning removed messages: %d -> %d", len(in), len(out))
	}
	if !strings.Contains(out[2].Content, "recall_tool_result") {
		t.Fatalf("oldest result not elided: %.80q", out[2].Content)
	}
	if compact.Estimate(out) >= compact.Estimate(in) {
		t.Fatal("pruning saved nothing")
	}
}

// The MCP chat's prune is an edit too (2.0 F3): an elided result loses its
// provider blocks; the messages it did not touch keep theirs.
func TestChatCompactorClearsBlocksOfPrunedResults(t *testing.T) {
	c := &chatCompactor{window: 64_000, meter: compact.NewMeter(0), store: &compact.Store{Dir: t.TempDir()}}
	pb, err := tui.NewProviderBlocks("k", []json.RawMessage{json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	in := toolTurns(12, 32_000)
	for i := range in {
		in[i] = tui.AttachProviderBlocks(in[i], pb)
	}
	out, _, changed := c.Compact(context.Background(), in, nil, false)
	if !changed {
		t.Fatal("nothing pruned")
	}
	if !strings.Contains(out[2].Content, "recall_tool_result") || out[2].ProviderBlocks != nil {
		t.Fatalf("pruned result kept its blocks: %.40q %+v", out[2].Content, out[2].ProviderBlocks)
	}
	if out[1].ProviderBlocks == nil || out[len(out)-1].ProviderBlocks == nil {
		t.Fatal("an unedited message lost its blocks")
	}
}

// Well under the window nothing is pruned, unless the loop forces it after
// a context-overflow error.
func TestChatCompactorOnlyForcedBelowTheWindow(t *testing.T) {
	c := &chatCompactor{window: 64_000, meter: compact.NewMeter(0), store: &compact.Store{Dir: t.TempDir()}}
	in := toolTurns(6, 8_000) // ~12k tokens
	if _, notes, changed := c.Compact(context.Background(), in, nil, false); changed || notes != nil {
		t.Fatalf("pruned below the threshold: %q", notes)
	}
	if _, _, changed := c.Compact(context.Background(), in, nil, true); !changed {
		t.Fatal("a forced compaction after an overflow pruned nothing")
	}
}

func TestNewChatLoopCompacts(t *testing.T) {
	env, cfg := mcpChatEnv(t)
	cfg.ContextLimit = 64_000
	l := newChatLoop(cfg, newChatClient(cfg, env.Registry, "sys"), env, "sys", "mcp-chat-test")
	c, ok := l.Compact.(*chatCompactor)
	if !ok {
		t.Fatalf("Compact = %T, want *chatCompactor", l.Compact)
	}
	if c.window != 64_000 || c.store == nil || c.meter == nil || c.meter.Overhead <= 0 {
		t.Fatalf("compactor = %+v", c)
	}
}
