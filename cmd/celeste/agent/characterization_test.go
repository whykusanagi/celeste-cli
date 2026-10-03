package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

func fakeRunner(t *testing.T, srv *fakeprovider.Server, mutate func(*Options)) (*Runner, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	opts := DefaultOptions()
	opts.Workspace = ws
	opts.EnablePlanning = false
	opts.RequireVerification = false
	opts.AutoApproveTools = true
	opts.RequestTimeout = 10 * time.Second
	if mutate != nil {
		mutate(&opts)
	}
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	r, err := NewRunner(cfg, opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r, ws
}

func TestAgentParallelTextFreeCallsThenComplete(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{
			{ID: "c1", Name: "read_file", Args: `{"path":"a.txt"}`},
			{ID: "c2", Name: "read_file", Args: `{"path":"b.txt"}`},
		}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: read both"},
	)
	r, ws := fakeRunner(t, srv, nil)
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	os.WriteFile(filepath.Join(ws, "b.txt"), []byte("beta"), 0o644)

	st, err := r.RunGoal(context.Background(), "read a.txt and b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "completed" || st.ToolCallCount != 2 {
		t.Fatalf("status=%q tool_calls=%d, want completed/2", st.Status, st.ToolCallCount)
	}
	// Second request must carry the assistant tool_calls message before both results (#203 class).
	body := srv.Requests()[1].Body
	msgs := body["messages"].([]any)
	roles := []string{}
	for _, m := range msgs {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if !strings.Contains(strings.Join(roles, ","), "assistant,tool,tool") {
		t.Fatalf("roles = %v, want an assistant tool_calls message followed by two tool results", roles)
	}
}

// 2.0 F2 intentional change (F2a plan Task 10; spec §4 F2, "agent runs gain the
// identical-call guard (3)"): the agent loop gained the identical-call guard.
// Before F2 this run went to MaxTurns (TestAgentIdenticalCallsRunToMaxTurns).
func TestAgentIdenticalCallsStopAtThree(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 6; i++ {
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c", Name: "read_file", Args: `{"path":"a.txt"}`}}})
	}
	srv := fakeprovider.NewOpenAI(t, turns...)
	r, ws := fakeRunner(t, srv, func(o *Options) { o.MaxTurns = 5 })
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)

	st, err := r.RunGoal(context.Background(), "loop")
	if err != nil {
		t.Fatal(err)
	}
	if st.Turn != 3 || st.Status != StatusNoProgressStopped || st.StopReason != "identical" {
		t.Fatalf("turn=%d status=%q stop=%q, want 3/no_progress_stopped/identical", st.Turn, st.Status, st.StopReason)
	}
	if got := len(srv.Requests()); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

// 2.0 F2 intentional change: the progress guard stops a loop whose
// arguments vary but whose results do not.
func TestAgentProgressGuardStopsArgsVaryingLoop(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 8; i++ {
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c", Name: "read_file", Args: fmt.Sprintf(`{"path":"a.txt","note":"%d"}`, i)}}})
	}
	srv := fakeprovider.NewOpenAI(t, turns...)
	r, ws := fakeRunner(t, srv, func(o *Options) { o.MaxTurns = 10 })
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)

	st, err := r.RunGoal(context.Background(), "loop")
	if err != nil {
		t.Fatal(err)
	}
	if st.Turn != 6 || st.Status != StatusNoProgressStopped || st.StopReason != "progress" {
		t.Fatalf("turn=%d status=%q stop=%q, want 6/no_progress_stopped/progress", st.Turn, st.Status, st.StopReason)
	}
}

func TestAgentProviderErrorMidLoop(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Status: 503, Body: `{"error":{"message":"overloaded"}}`},
		fakeprovider.Turn{Status: 503, Body: `{"error":{"message":"overloaded"}}`},
		fakeprovider.Turn{Status: 503, Body: `{"error":{"message":"overloaded"}}`},
		fakeprovider.Turn{Status: 503, Body: `{"error":{"message":"overloaded"}}`},
	)
	r, ws := fakeRunner(t, srv, nil)
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := r.RunGoal(ctx, "read a.txt")
	if ctx.Err() != nil {
		t.Fatal("run hung instead of ending on a provider error")
	}
	// Today's deterministic behaviour: one successful tool-call turn, then the
	// second turn hits the 503 and the run fails outright (no further retries
	// inside RunGoal beyond whatever the llm.Client itself already did per
	// request) — 1 success + 3 attempts (1 initial + 2 retries, 1s+2s backoff)
	// = 4 requests total, turn 2, status failed.
	if err == nil {
		t.Fatal("want a non-nil error from the provider outage")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v, want it to mention the 503 status", err)
	}
	if st == nil || st.Status != "failed" {
		t.Fatalf("status = %+v, want failed", st)
	}
	if st.Turn != 2 {
		t.Fatalf("turn = %d, want 2", st.Turn)
	}
	if got := len(srv.Requests()); got != 4 {
		t.Fatalf("requests = %d, want 4 (1 success + 3 attempts: 1 initial + 2 retries before giving up)", got)
	}
}
