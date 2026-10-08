package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
)

type sink struct {
	mu   sync.Mutex
	list []string
}

func (s *sink) add(m string) { s.mu.Lock(); s.list = append(s.list, m); s.mu.Unlock() }
func (s *sink) all() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.list, "\n")
}

// A Stop hook's "deny" keeps a finished run going once, with its reason as
// the next instruction; a second deny is reported and ignored.
func TestAgentStopHookContinuesOnce(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "TASK_COMPLETE: too early"},
		fakeprovider.Turn{Text: "TASK_COMPLETE: verified"},
	)
	warns := &sink{}
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.Warn = warns.add
		home, _ := os.UserHomeDir() // fakeRunner already pointed HOME at a temp dir
		writeHooks(t, home, map[string]any{"event": "Stop", "command": hooktest.Command(t, "deny", "run the tests first")})
	})
	st, err := r.RunGoal(context.Background(), "do it")
	if err != nil || st.Status != StatusCompleted || st.LastAssistantResponse != "TASK_COMPLETE: verified" {
		t.Fatalf("status=%q last=%q err=%v", st.Status, st.LastAssistantResponse, err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 || !strings.Contains(toJSONString(reqs[1].Body["messages"]), "run the tests first") {
		t.Fatalf("requests=%d, want the Stop reason in the second request", len(reqs))
	}
	if !strings.Contains(warns.all(), "one continuation per run") {
		t.Fatalf("warnings = %q, want the ignored second deny reported", warns.all())
	}
}

func TestAgentNestedRunSkipsStop(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.Nested = true
		home, _ := os.UserHomeDir()
		writeHooks(t, home, map[string]any{"event": "Stop", "command": hooktest.Command(t, "deny", "keep going")})
	})
	if st, err := r.RunGoal(context.Background(), "do it"); err != nil || st.Status != StatusCompleted || len(srv.Requests()) != 1 {
		t.Fatalf("status=%q err=%v requests=%d", st.Status, err, len(srv.Requests()))
	}
}

// loadHooks loads a hooks runner from a temp home holding defs.
func loadHooks(t *testing.T, defs ...map[string]any) *hooks.Runner {
	t.Helper()
	home := isolateHome(t)
	writeHooks(t, home, defs...)
	r, err := hooks.Load(hooks.Options{Workspace: t.TempDir(), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// PreCompact runs only before a summary (as in the TUI); a block skips the
// summary and is always reported through the warning sink.
func TestAgentPreCompactBlocksSummary(t *testing.T) {
	backend := &windowBackend{window: 64_000, turns: 20}
	runner, _ := newCompactionRunner(t, backend, 64_000)
	runner.pruned = nil // pruning off: the summary rung is the only one
	runner.hooks = loadHooks(t, map[string]any{"event": "PreCompact", "command": hooktest.Command(t, "deny", "not now")})
	warns := &sink{}
	runner.warn = warns.add
	summaries := 0
	runner.summarize = func(context.Context, string, string) (string, error) {
		summaries++
		return "## Goal\nread every file", nil
	}
	_, _ = runner.RunGoal(context.Background(), "read every file") // may end in an overflow: compaction was refused
	if summaries != 0 {
		t.Fatalf("summaries = %d, want the PreCompact hook to block them", summaries)
	}
	if !strings.Contains(warns.all(), "compaction blocked by a PreCompact hook: not now") {
		t.Fatalf("warnings = %q", warns.all())
	}
}

// A PreCompact reason is hook output: the warning that carries it reaches
// sinks that show it as is (the orchestrator's action feed, the TUI /agent
// system line), so it is escaped where it becomes display text.
func TestAgentPreCompactReasonEscapedInWarning(t *testing.T) {
	backend := &windowBackend{window: 64_000, turns: 20}
	runner, _ := newCompactionRunner(t, backend, 64_000)
	runner.pruned = nil
	runner.hooks = loadHooks(t, map[string]any{"event": "PreCompact", "command": hooktest.Command(t, "canned", "escape-deny")})
	warns := &sink{}
	runner.warn = warns.add
	runner.summarize = func(context.Context, string, string) (string, error) { return "## Goal\nx", nil }
	_, _ = runner.RunGoal(context.Background(), "read every file")
	got := warns.all()
	if !strings.Contains(got, "compaction blocked by a PreCompact hook: ") {
		t.Fatalf("warnings = %q, want the block reported", got)
	}
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\a") {
		t.Fatalf("warnings = %q, want the hook's escape sequences escaped", got)
	}
}

func TestAgentCompactionHooksAroundSummary(t *testing.T) {
	backend := &windowBackend{window: 64_000, turns: 20}
	runner, _ := newCompactionRunner(t, backend, 64_000)
	runner.pruned = nil
	record := filepath.Join(t.TempDir(), "post.json")
	runner.hooks = loadHooks(t,
		map[string]any{"event": "PreCompact", "command": hooktest.Command(t, "context", "keep the todo list")},
		map[string]any{"event": "PostCompact", "command": hooktest.Command(t, "record", record)},
	)
	var mu sync.Mutex
	var sawInstructions bool
	runner.summarize = func(_ context.Context, _, user string) (string, error) {
		mu.Lock()
		sawInstructions = sawInstructions || strings.Contains(user, "Additional instructions from a PreCompact hook:\nkeep the todo list")
		mu.Unlock()
		return "## Goal\nread every file\n## Progress\n### Done\nsummary-probe", nil
	}
	st, err := runner.RunGoal(context.Background(), "read every file")
	if err != nil || st.Status != StatusCompleted {
		t.Fatalf("status=%q err=%v", st.Status, err)
	}
	if !sawInstructions {
		t.Fatal("PreCompact additionalContext should reach the summary request")
	}
	if b, _ := os.ReadFile(record); !strings.Contains(string(b), "summary-probe") || !strings.Contains(string(b), "auto") {
		t.Fatalf("PostCompact payload = %s, want the summary text and trigger auto", b)
	}
}

// A Stop deny is honoured only while turns remain.
func TestAgentStopHookNeedsATurnLeft(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	warns := &sink{}
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.MaxTurns = 1
		o.Warn = warns.add
		home, _ := os.UserHomeDir()
		writeHooks(t, home, map[string]any{"event": "Stop", "command": hooktest.Command(t, "deny", "keep going")})
	})
	st, err := r.RunGoal(context.Background(), "do it")
	if err != nil || st.Status != StatusCompleted || len(srv.Requests()) != 1 {
		t.Fatalf("status=%q err=%v requests=%d", st.Status, err, len(srv.Requests()))
	}
	if !strings.Contains(warns.all(), "no turns left") {
		t.Fatalf("warnings = %q, want the refused continuation reported", warns.all())
	}
}

// Stop fires when a run finishes as completed, not when it ends another way
// (here: the turn cap).
func TestAgentStopHookSkippedWhenRunDoesNotComplete(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "still thinking"},
		fakeprovider.Turn{Text: "still thinking"},
	)
	record := filepath.Join(t.TempDir(), "stop.json")
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.MaxTurns = 2
		home, _ := os.UserHomeDir()
		writeHooks(t, home, map[string]any{"event": "Stop", "command": hooktest.Command(t, "record", record)})
	})
	st, err := r.RunGoal(context.Background(), "do it")
	if err != nil || st.Status == StatusCompleted {
		t.Fatalf("status=%q err=%v, want a run that does not complete", st.Status, err)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("Stop hook fired for a run that did not complete (stat err=%v)", err)
	}
}

// After Close no hook fires on the caller's behalf (callbackGate rule).
func TestAgentClosedRunnerFiresNoHooks(t *testing.T) {
	record := filepath.Join(t.TempDir(), "hook.json")
	r := &Runner{
		hooks: loadHooks(t,
			map[string]any{"event": "Stop", "command": hooktest.Command(t, "record", record)},
			map[string]any{"event": "PreCompact", "command": hooktest.Command(t, "record", record)},
			map[string]any{"event": "PostCompact", "command": hooktest.Command(t, "record", record)},
		),
		gate: &callbackGate{},
	}
	r.Close()
	continued := false
	if next := r.stopHook(context.Background(), &RunState{Options: Options{MaxTurns: 5}}, &continued); next != "" {
		t.Fatalf("stopHook after Close = %q", next)
	}
	sum, _ := r.hookedSummarize(func(context.Context, string, string) (string, error) { return "s", nil })
	if _, err := sum(context.Background(), "", ""); err != nil {
		t.Fatal(err)
	}
	r.postCompact(context.Background(), "s")
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("a hook fired after Close (stat err=%v)", err)
	}
}

// A subagent (nested, with an AgentID) fires SubagentStop instead of Stop,
// and its deny continues the run once, like Stop's.
func TestAgentSubagentStopContinuesOnce(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "TASK_COMPLETE: too early"},
		fakeprovider.Turn{Text: "TASK_COMPLETE: verified"},
	)
	warns := &sink{}
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.Nested = true
		o.AgentID = "sub-1-1"
		o.Warn = warns.add
		home, _ := os.UserHomeDir()
		writeHooks(t, home,
			map[string]any{"event": "SubagentStop", "command": hooktest.Command(t, "deny", "check the diff first")},
			map[string]any{"event": "Stop", "command": hooktest.Command(t, "deny", "stop hook must not fire")},
		)
	})
	st, err := r.RunGoal(context.Background(), "do it")
	if err != nil || st.Status != StatusCompleted || st.LastAssistantResponse != "TASK_COMPLETE: verified" {
		t.Fatalf("status=%q last=%q err=%v", st.Status, st.LastAssistantResponse, err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 || !strings.Contains(toJSONString(reqs[1].Body["messages"]), "check the diff first") {
		t.Fatalf("requests=%d, want the SubagentStop reason in the second request", len(reqs))
	}
	if strings.Contains(toJSONString(reqs[1].Body["messages"]), "stop hook must not fire") {
		t.Fatal("a subagent fired Stop")
	}
	if !strings.Contains(warns.all(), "a SubagentStop hook asked the agent to continue again") {
		t.Fatalf("warnings = %q, want the ignored second deny reported", warns.all())
	}
}

// SubagentStop's payload names the subagent and carries its last message.
func TestAgentSubagentStopPayload(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: sub done"})
	record := filepath.Join(t.TempDir(), "substop.json")
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.Nested = true
		o.AgentID = "sub-7-2"
		home, _ := os.UserHomeDir()
		writeHooks(t, home, map[string]any{"event": "SubagentStop", "command": hooktest.Command(t, "record", record)})
	})
	if _, err := r.RunGoal(context.Background(), "do it"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("SubagentStop did not fire: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if p["event"] != "SubagentStop" || p["agent_id"] != "sub-7-2" || p["last_message"] != "TASK_COMPLETE: sub done" {
		t.Fatalf("payload = %v", p)
	}
}

// Orchestrator lanes and the TUI's /agent are nested without an AgentID:
// neither Stop nor SubagentStop fires.
func TestAgentNestedWithoutAgentIDSkipsSubagentStop(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	record := filepath.Join(t.TempDir(), "substop.json")
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.Nested = true
		home, _ := os.UserHomeDir()
		writeHooks(t, home, map[string]any{"event": "SubagentStop", "command": hooktest.Command(t, "record", record)})
	})
	if _, err := r.RunGoal(context.Background(), "do it"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("SubagentStop fired for a nested run without an AgentID (stat err=%v)", err)
	}
}
