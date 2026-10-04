package agent

import (
	"context"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// #317: a cancelled run that resumes and completes carries no stale error,
// in the returned state or the stored checkpoint.
func TestResumeOfCancelledRunClearsOldError(t *testing.T) {
	isolateHome(t)
	ws := t.TempDir()
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r := resumeRunner(t, srv, ws, nil)

	state := NewRunState("finish the thing", r.options)
	normalizeStateOptions(state, r.options)
	state.Phase = PhaseExecution
	state.Turn = 1
	state.Status = StatusCancelled
	state.Error = "run cancelled: context canceled"
	now := time.Now()
	state.CompletedAt = &now
	state.Messages = append(state.Messages, tui.ChatMessage{Role: "user", Content: "finish the thing", Timestamp: now})
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}

	got, err := r.Resume(context.Background(), state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCompleted || got.Error != "" {
		t.Fatalf("resume: status=%q error=%q, want completed with no error", got.Status, got.Error)
	}
	saved, err := r.store.Load(state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != StatusCompleted || saved.Error != "" {
		t.Fatalf("saved: status=%q error=%q, want completed with no error", saved.Status, saved.Error)
	}
}

// The stale error goes on resume, not only on completion: a resumed
// cancelled run that stops for another reason reports that reason alone.
func TestResumeOfCancelledRunAtCapDropsOldError(t *testing.T) {
	isolateHome(t)
	srv := fakeprovider.NewOpenAI(t)
	r := resumeRunner(t, srv, t.TempDir(), func(o *Options) { o.MaxTurns = 2 })

	state := NewRunState("finish the thing", r.options)
	normalizeStateOptions(state, r.options)
	state.Phase = PhaseExecution
	state.Turn = 2
	state.Status = StatusCancelled
	state.Error = "run cancelled: context canceled"
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}

	got, err := r.Resume(context.Background(), state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusMaxTurnsReached || got.Error != "" {
		t.Fatalf("resume: status=%q error=%q, want max_turns_reached with no error", got.Status, got.Error)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}
