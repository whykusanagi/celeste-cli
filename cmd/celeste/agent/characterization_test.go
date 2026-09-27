package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
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

// Baseline: the agent loop has NO identical-call guard today. A model that
// repeats the same call runs until MaxTurns. 2.0 F2 adds the guard (spec §4
// F2 "intentional behaviour changes"); update this test then.
func TestAgentIdenticalCallsRunToMaxTurns(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 6; i++ {
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c", Name: "read_file", Args: `{"path":"a.txt"}`}}})
	}
	srv := fakeprovider.NewOpenAI(t, turns...)
	r, ws := fakeRunner(t, srv, func(o *Options) { o.MaxTurns = 5 })
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)

	st, _ := r.RunGoal(context.Background(), "loop")
	if st.Turn != 5 {
		t.Fatalf("turn = %d, want 5 (no identical-call guard in the agent loop today)", st.Turn)
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
	if err == nil && (st == nil || st.Status == "completed") {
		t.Fatalf("want a failed run, got status %v err %v", st, err)
	}
}
