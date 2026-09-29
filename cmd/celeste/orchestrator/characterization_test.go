package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// orchWorkspace isolates HOME and runs the test from a fresh workspace (the
// lanes use the working directory).
func orchWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	t.Chdir(ws)
	return ws
}

// writeScript is one lane that writes out.txt. agent.DefaultOptions() has
// EnablePlanning on, so a planning turn comes first, then the write_file
// call, then a completion turn.
func writeScript(t *testing.T) *fakeprovider.Server {
	return fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Write hi to out.txt"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: wrote it"},
	)
}

func fakeOrchCfg(srv *fakeprovider.Server) *config.Config {
	return &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
}

type eventLog struct {
	mu   sync.Mutex
	list []OrchestratorEvent
}

func (l *eventLog) add(e OrchestratorEvent) { l.mu.Lock(); l.list = append(l.list, e); l.mu.Unlock() }
func (l *eventLog) texts() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	for _, e := range l.list {
		b.WriteString(e.Text + "\n")
	}
	return b.String()
}

// toolResult is the last tool-result message in request i.
func toolResult(t *testing.T, srv *fakeprovider.Server, i int) string {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) <= i {
		t.Fatalf("got %d requests, want more than %d", len(reqs), i)
	}
	msgs, _ := reqs[i].Body["messages"].([]any)
	content := ""
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" {
			content, _ = mm["content"].(string)
		}
	}
	if content == "" {
		t.Fatalf("no tool-result message in request %d", i)
	}
	return content
}

func runOrch(t *testing.T, o *Orchestrator, goal string) (*Result, *eventLog) {
	t.Helper()
	log := &eventLog{}
	o.OnEvent(log.add)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	res, err := o.Run(ctx, goal)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res, log
}

// Flipped in F2c Task 6 (spec §3.2): this was
// TestOrchestratorSilentlyDeniesMutatingTools, the baseline of a bug where
// the default runner set neither a prompt nor auto-approve. A headless
// orchestrator (no WithPrompt, no WithTrust) still denies tools that need
// approval, but no longer silently: Run says so in an event and records it
// in Result.Approval.
func TestOrchestratorHeadlessDeniesAndSaysSo(t *testing.T) {
	ws := orchWorkspace(t)
	srv := writeScript(t)
	res, log := runOrch(t, New(fakeOrchCfg(srv)), "write hi to out.txt")

	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err == nil {
		t.Fatal("out.txt was written by a headless orchestrator")
	}
	if c := toolResult(t, srv, 2); !strings.Contains(c, "Permission denied") || !strings.Contains(c, "no prompt is configured") {
		t.Fatalf("tool result = %q, want the headless denial", c)
	}
	if !strings.Contains(log.texts(), headlessDenyNotice) {
		t.Fatalf("events lack the headless-deny notice:\n%s", log.texts())
	}
	if res.Approval != ApprovalDeny || res.Approval.String() != "deny" {
		t.Fatalf("Result.Approval = %v, want deny", res.Approval)
	}
}

// The caller's prompt (the TUI modal) answers the lane's Ask (spec §4 F2:
// "orchestrator: inherits from its caller").
func TestOrchestratorAsksTheCallersPrompt(t *testing.T) {
	ws := orchWorkspace(t)
	srv := writeScript(t)
	var mu sync.Mutex
	var asked []string
	prompt := func(req tools.PermissionRequest) tools.PermissionResponse {
		mu.Lock()
		asked = append(asked, req.ToolName)
		mu.Unlock()
		return tools.PermissionResponse{Decision: "allow_once"}
	}
	res, log := runOrch(t, New(fakeOrchCfg(srv), WithPrompt(prompt)), "write hi to out.txt")

	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err != nil {
		t.Fatalf("out.txt not written after the prompt allowed it: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0] != "write_file" {
		t.Fatalf("prompt asked for %v, want [write_file]", asked)
	}
	if res.Approval != ApprovalPrompt || strings.Contains(log.texts(), headlessDenyNotice) {
		t.Fatalf("Approval = %v; a prompted run must not print the headless notice", res.Approval)
	}
}

// Review Focus 5: a "deny" in the modal reaches the model and the run ends.
func TestOrchestratorPromptDenyFinishesTheRun(t *testing.T) {
	ws := orchWorkspace(t)
	srv := writeScript(t)
	deny := func(tools.PermissionRequest) tools.PermissionResponse {
		return tools.PermissionResponse{Decision: "deny"}
	}
	runOrch(t, New(fakeOrchCfg(srv), WithPrompt(deny)), "write hi to out.txt")
	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err == nil {
		t.Fatal("out.txt was written after the user denied it")
	}
	// The tool result is JSON, so the quotes around the name are escaped.
	if c := toolResult(t, srv, 2); !strings.Contains(c, `user denied execution of \"write_file\"`) {
		t.Fatalf("tool result = %q, want the user's denial", c)
	}
}

// WithTrust: invoking the orchestrator is the approval (no prompt).
func TestOrchestratorTrustWritesWithoutPrompt(t *testing.T) {
	ws := orchWorkspace(t)
	srv := writeScript(t)
	res, _ := runOrch(t, New(fakeOrchCfg(srv), WithTrust()), "write hi to out.txt")
	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err != nil {
		t.Fatalf("out.txt not written under WithTrust: %v", err)
	}
	if res.Approval != ApprovalTrust {
		t.Fatalf("Result.Approval = %v, want trust", res.Approval)
	}
}

// The primary and reviewer lanes nest under one environment per run: repo
// hooks are loaded (and reported as untrusted) once, not per lane.
func TestOrchestratorLanesShareOneEnvironment(t *testing.T) {
	ws := orchWorkspace(t)
	if err := os.MkdirAll(filepath.Join(ws, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".celeste", "hooks.json"), []byte(`{"hooks":[{"event":"Stop","command":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Fix it"},            // primary: plan
		fakeprovider.Turn{Text: "TASK_COMPLETE: fixed"}, // primary: done
		fakeprovider.Turn{Text: "1. Review it"},         // reviewer: plan
		fakeprovider.Turn{Text: "TASK_COMPLETE: []"},    // reviewer: no issues
	)
	cfg := fakeOrchCfg(srv)
	cfg.Orchestrator = &config.OrchestratorConfig{Lanes: map[string]config.LaneConfig{
		"code": {Primary: "fake-model", Reviewer: "fake-model"},
	}}
	res, log := runOrch(t, New(cfg, WithTrust()), "fix the bug in main.go")
	if res.Verdict == nil {
		t.Fatal("the reviewer lane did not run")
	}
	if n := strings.Count(log.texts(), "skipping"); n != 1 {
		t.Fatalf("hooks loaded %d times across lanes, want once:\n%s", n, log.texts())
	}
}

// C2: the run's lane environment is closed before the terminal event, so
// nothing from it can reach a caller that stops reading there (the TUI).
func TestOrchestratorClosesLanesBeforeTheTerminalEvent(t *testing.T) {
	orchWorkspace(t)
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Say hi"},
		fakeprovider.Turn{Text: "TASK_COMPLETE: hi"},
	)
	var lanes *loop.Parent
	orig := newLanes
	t.Cleanup(func() { newLanes = orig })
	newLanes = func(cfg *config.Config, ws string, opts loop.SetupOptions) *loop.Parent {
		lanes = orig(cfg, ws, opts)
		return lanes
	}
	o := New(fakeOrchCfg(srv), WithTrust())
	var atEnd error
	o.OnEvent(func(e OrchestratorEvent) {
		if e.Kind == EventComplete {
			child, err := lanes.Nested(loop.NestedOptions{})
			if err == nil {
				child.Close()
			}
			atEnd = err
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := o.Run(ctx, "say hi"); err != nil {
		t.Fatal(err)
	}
	if atEnd == nil || !strings.Contains(atEnd.Error(), "closed") {
		t.Fatalf("at EventComplete the lanes' environment was still open (Nested err = %v)", atEnd)
	}
}

// A lane whose primary has its own endpoint (cross-provider orchestration)
// answers the reviewer's critique from that endpoint too: the defense turn
// used to drop PrimaryBaseURL/PrimaryAPIKey and hit the main config's.
func TestOrchestratorDefenseUsesThePrimarysEndpoint(t *testing.T) {
	orchWorkspace(t)
	primary := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Fix it"},              // primary: plan
		fakeprovider.Turn{Text: "TASK_COMPLETE: fixed"},   // primary: done
		fakeprovider.Turn{Text: "1. Address the review"},  // defense: plan
		fakeprovider.Turn{Text: "TASK_COMPLETE: revised"}, // defense: done
	)
	main := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Review it"}, // reviewer: plan
		fakeprovider.Turn{Text: `TASK_COMPLETE: [{"file":"main.go","line":1,"severity":"high","description":"broken"}]`},
	)
	cfg := fakeOrchCfg(main)
	cfg.Orchestrator = &config.OrchestratorConfig{DebateRounds: 1, Lanes: map[string]config.LaneConfig{
		"code": {Primary: "fake-primary", PrimaryBaseURL: primary.BaseURL(), PrimaryAPIKey: "pk", Reviewer: "fake-model"},
	}}
	_, log := runOrch(t, New(cfg, WithTrust()), "fix the bug in main.go")
	if !strings.Contains(log.texts(), "revised") {
		t.Fatalf("no defense turn in the events:\n%s", log.texts())
	}
	if n := len(primary.Requests()); n != 4 {
		t.Fatalf("the primary's endpoint got %d requests, want 4 (primary and defense)", n)
	}
	if n := len(main.Requests()); n != 2 {
		t.Fatalf("the main endpoint got %d requests, want the reviewer's 2", n)
	}
}
