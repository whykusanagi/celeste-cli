package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// Verbose output and progress callbacks keep their shape on the loop.
func TestAgentVerboseOutputAndProgressOnTheLoop(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: read it"},
	)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	opts := DefaultOptions()
	opts.Workspace = ws
	opts.EnablePlanning = false
	opts.AutoApproveTools = true
	opts.Verbose = true
	var kinds []ProgressKind
	opts.OnProgress = func(k ProgressKind, _ string, _, _ int) { kinds = append(kinds, k) }
	var stats []TurnStats
	opts.OnTurnStats = func(s TurnStats) { stats = append(stats, s) }
	var out bytes.Buffer
	r, err := NewRunner(&config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}, opts, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	st, err := r.RunGoal(context.Background(), "read a.txt")
	if err != nil || st.Status != StatusCompleted {
		t.Fatalf("status=%q err=%v", st.Status, err)
	}
	for _, want := range []string{"[agent] turn 1/50", "[tool] read_file", "[agent] turn 2/50", "[assistant]\nTASK_COMPLETE: read it"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if len(stats) != 2 || stats[0].ToolCalls[0] != "read_file" {
		t.Fatalf("turn stats = %+v", stats)
	}
	if kinds[0] != ProgressTurnStart || kinds[len(kinds)-1] != ProgressComplete {
		t.Fatalf("progress kinds = %v", kinds)
	}
	var toolSteps int
	for _, s := range st.Steps {
		if s.Type == "tool" && s.Name == "read_file" && s.ToolCall == "c1" {
			toolSteps++
		}
	}
	if toolSteps != 1 || st.ToolCallCount != 1 {
		t.Fatalf("steps=%+v tool_calls=%d", st.Steps, st.ToolCallCount)
	}
}

// The TUI's /agent permission modal answers through the loop's Gate.
func TestAgentPromptFuncIsTheGate(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: wrote"},
	)
	asked := 0
	r, ws := fakeRunner(t, srv, func(o *Options) {
		o.AutoApproveTools = false
		o.PromptFunc = func(tools.PermissionRequest) tools.PermissionResponse {
			asked++
			return tools.PermissionResponse{Decision: "allow_once"}
		}
	})
	if _, err := r.RunGoal(context.Background(), "write"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "out.txt")); err != nil || string(b) != "hi" || asked != 1 {
		t.Fatalf("asked=%d file=%q err=%v", asked, b, err)
	}
}

// A cancelled run reports cancelled, and its history stays paired.
func TestAgentInterruptIsCancelled(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "unused"})
	r, _ := fakeRunner(t, srv, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st, err := r.RunGoal(ctx, "anything")
	if err == nil || st.Status != StatusCancelled {
		t.Fatalf("status=%q err=%v", st.Status, err)
	}
}

// blockingBackend holds every request until its context ends.
type blockingBackend struct{ started chan struct{} }

func (b *blockingBackend) SendMessageStreamEvents(ctx context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, _ llm.StreamEventCallback) error {
	close(b.started)
	<-ctx.Done()
	return ctx.Err()
}
func (b *blockingBackend) SendMessageStream(context.Context, []tui.ChatMessage, []tui.SkillDefinition, llm.StreamCallback) error {
	return nil
}
func (b *blockingBackend) SendMessageSync(context.Context, []tui.ChatMessage, []tui.SkillDefinition) (*llm.ChatCompletionResult, error) {
	return nil, context.Canceled
}
func (b *blockingBackend) SetSystemPrompt(string)               {}
func (b *blockingBackend) SetThinkingConfig(llm.ThinkingConfig) {}
func (b *blockingBackend) Close() error                         { return nil }

// 2.0 F2 intentional change: a run cancelled mid-request reports cancelled
// (before the loop it reported failed).
func TestAgentCancelMidRequestIsCancelled(t *testing.T) {
	isolateHome(t)
	be := &blockingBackend{started: make(chan struct{})}
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.Client = llm.NewClientWithBackend(&llm.Config{Model: "fake"}, nil, be)
	opts.EnablePlanning = false
	opts.RequireVerification = false
	r, err := NewRunner(&config.Config{Model: "fake", BaseURL: "http://127.0.0.1:1"}, opts, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-be.started
		cancel()
	}()
	st, err := r.RunGoal(ctx, "anything")
	if err == nil || st.Status != StatusCancelled || st.Turn != 1 {
		t.Fatalf("status=%q turn=%d err=%v, want cancelled at turn 1", st.Status, st.Turn, err)
	}
}
