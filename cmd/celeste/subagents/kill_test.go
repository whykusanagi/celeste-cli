package subagents

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// blockingExecFor returns an execFunc that blocks until its context is cancelled,
// then marks the run failed — simulating a long-running subagent that honors ctx
// (the post-349f1f14 runtime). signalReady fires once it has started.
func blockingExecFor(m *Manager, started chan<- struct{}) execFunc {
	return func(ctx context.Context, run *SubagentRun, _, _ string, _ TurnCallback, _ int, _ bool) (*SubagentRun, error) {
		close(started)
		<-ctx.Done()
		m.mu.Lock()
		if run.Status != "failed" {
			run.Status = "cancelled"
		}
		run.EndedAt = time.Now()
		m.mu.Unlock()
		return run, ctx.Err()
	}
}

// Kill must cancel a specific in-flight (backgrounded) run and mark it failed.
func TestKill_CancelsRunningSubagent(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	started := make(chan struct{})
	m.execFn = blockingExecFor(m, started)

	// Background almost immediately so SpawnWithOptions returns the live run.
	run, err := m.SpawnWithOptions(context.Background(), "long task", "/tmp", SpawnOptions{
		BackgroundAfter: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	<-started // ensure the exec goroutine is in flight

	if !m.Kill(run.ID) {
		t.Fatalf("Kill returned false for an in-flight run %s", run.ID)
	}

	// The run should reach a terminal state promptly (ctx cancelled).
	deadline := time.After(2 * time.Second)
	for {
		m.mu.Lock()
		status := run.Status
		m.mu.Unlock()
		if status == "failed" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("run did not reach failed status after Kill, got %q", status)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// Kill must resolve the on-screen name (element like "water" or the Romaji like
// "mizu" from the "水 mizu" display name) — what the user actually sees in /agents
// — not just the internal run id (#d15ac448).
func TestKill_ByOnScreenName(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	started := make(chan struct{})
	m.execFn = blockingExecFor(m, started)

	run, err := m.SpawnWithOptions(context.Background(), "task", "/tmp", SpawnOptions{
		BackgroundAfter: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	<-started

	// The user types the Romaji shown on screen (e.g. "mizu"), not the sub-… id.
	parts := strings.Fields(run.Name)
	onScreen := parts[len(parts)-1]
	if !m.Kill(onScreen) {
		t.Fatalf("Kill by on-screen name %q failed (Name=%q Element=%q)", onScreen, run.Name, run.Element)
	}

	deadline := time.After(2 * time.Second)
	for {
		m.mu.Lock()
		status := run.Status
		m.mu.Unlock()
		if status == "failed" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("run not failed after Kill(%q), got %q", onScreen, status)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// Kill must also resolve the english element name ("water").
func TestKill_ByElementName(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	started := make(chan struct{})
	m.execFn = blockingExecFor(m, started)

	run, err := m.SpawnWithOptions(context.Background(), "task", "/tmp", SpawnOptions{
		BackgroundAfter: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	<-started
	if run.Element == "" {
		t.Skip("run has no element name to match")
	}
	if !m.Kill(run.Element) {
		t.Fatalf("Kill by element %q failed", run.Element)
	}
}

// A run with a task_id is registered under both its id and task_id; ListRuns
// must return it once, not twice (the /agents duplicate-row bug).
func TestListRuns_DedupesTaskIDRuns(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	m.execFn = fakeExecFor(m, time.Millisecond)
	if _, err := m.SpawnWithOptions(context.Background(), "task", "/tmp", SpawnOptions{TaskID: "summarizer"}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if got := len(m.ListRuns()); got != 1 {
		t.Fatalf("expected 1 deduped run, got %d", got)
	}
}

// Kill on an unknown id returns false rather than panicking.
func TestKill_UnknownIDReturnsFalse(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	if m.Kill("does-not-exist") {
		t.Fatal("Kill should return false for an unknown id")
	}
}

// Concurrent Kill + ListRuns + completion must be race-free (run under -race).
// Guards the per-run cancel map and the run-status writes the security review
// flagged (registerCancel/clearCancel/Kill/ListRuns all touch shared state).
func TestKill_ConcurrentWithListRunsRaceFree(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	started := make(chan struct{}, 4)
	m.execFn = func(ctx context.Context, run *SubagentRun, _, _ string, _ TurnCallback, _ int, _ bool) (*SubagentRun, error) {
		started <- struct{}{}
		<-ctx.Done()
		m.mu.Lock()
		if run.Status != "failed" {
			run.Status = "cancelled"
		}
		run.EndedAt = time.Now()
		m.mu.Unlock()
		return run, ctx.Err()
	}

	var runs []*SubagentRun
	for i := 0; i < 4; i++ {
		r, err := m.SpawnWithOptions(context.Background(), "task", "/tmp", SpawnOptions{
			BackgroundAfter: time.Millisecond,
		})
		if err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}
		runs = append(runs, r)
	}
	for i := 0; i < 4; i++ {
		<-started
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			_ = m.ListRuns()
		}
		close(done)
	}()
	for _, r := range runs {
		m.Kill(r.ID)
	}
	<-done
}
