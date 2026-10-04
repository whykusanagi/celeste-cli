package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// resumeRunner builds a runner on srv sharing the caller's HOME (and so its
// checkpoint store), unlike fakeRunner, which gives every runner a new HOME.
func resumeRunner(t *testing.T, srv *fakeprovider.Server, ws string, mutate func(*Options)) *Runner {
	t.Helper()
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
	return r
}

func readTurn(i int) fakeprovider.Turn {
	return fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: fmt.Sprintf("c%d", i), Name: "read_file", Args: fmt.Sprintf(`{"path":"a.txt","note":"%d"}`, i)}}}
}

// #316: a run that stopped at max_turns_reached resumes with a larger
// explicit --max-turns and continues to completion.
func TestResumeAfterMaxTurnsHonoursLargerExplicitLimit(t *testing.T) {
	isolateHome(t)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := fakeprovider.NewOpenAI(t, readTurn(1), readTurn(2))
	first := resumeRunner(t, srv, ws, func(o *Options) { o.MaxTurns = 2 })
	st, err := first.RunGoal(context.Background(), "read a.txt a few times")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusMaxTurnsReached || st.Turn != 2 {
		t.Fatalf("first run: status=%q turn=%d, want max_turns_reached/2", st.Status, st.Turn)
	}

	srv.Push(readTurn(3), fakeprovider.Turn{Text: "TASK_COMPLETE: read it"})
	second := resumeRunner(t, srv, ws, func(o *Options) {
		o.MaxTurns = 5
		o.MaxTurnsExplicit = true
	})
	got, err := second.Resume(context.Background(), st.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCompleted || got.Turn != 4 || got.Options.MaxTurns != 5 {
		t.Fatalf("resume: status=%q turn=%d max=%d, want completed/4/5", got.Status, got.Turn, got.Options.MaxTurns)
	}
	saved, err := second.store.Load(st.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Options.MaxTurns != 5 || saved.Status != StatusCompleted {
		t.Fatalf("saved: status=%q max=%d, want completed/5", saved.Status, saved.Options.MaxTurns)
	}
}

// Without an explicit flag the saved limit still wins: a resumer's default
// MaxTurns (subagents, /agent resume) must not silently change a run's cap.
func TestResumeKeepsSavedMaxTurnsWithoutExplicitFlag(t *testing.T) {
	isolateHome(t)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := fakeprovider.NewOpenAI(t, readTurn(1), readTurn(2))
	first := resumeRunner(t, srv, ws, func(o *Options) { o.MaxTurns = 2 })
	st, err := first.RunGoal(context.Background(), "read a.txt a few times")
	if err != nil {
		t.Fatal(err)
	}
	second := resumeRunner(t, srv, ws, func(o *Options) { o.MaxTurns = 50 })
	got, err := second.Resume(context.Background(), st.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusMaxTurnsReached || got.Turn != 2 || got.Options.MaxTurns != 2 {
		t.Fatalf("resume: status=%q turn=%d max=%d, want max_turns_reached/2/2", got.Status, got.Turn, got.Options.MaxTurns)
	}
	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("requests = %d, want 2 (resume must not call the model)", n)
	}
}
