package subagents

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func TestNewManager(t *testing.T) {
	cfg := &config.Config{APIKey: "test"}
	m := NewManager(cfg, "/tmp/test", false)
	if m.cfg != cfg {
		t.Fatal("config not set")
	}
	if m.workspace != "/tmp/test" {
		t.Fatalf("workspace = %q, want /tmp/test", m.workspace)
	}
	if m.isChild {
		t.Fatal("isChild should be false")
	}
	if len(m.runs) != 0 {
		t.Fatal("runs should be empty")
	}
}

func TestNewManagerChild(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", true)
	if !m.isChild {
		t.Fatal("isChild should be true")
	}
}

func TestSpawnBlocksRecursionWhenChild(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", true)
	_, err := m.Spawn(t.Context(), "do something", "")
	if err == nil {
		t.Fatal("expected error for recursive spawning")
	}
	if err.Error() != "recursive subagent spawning is not allowed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpawnBlocksRecursionMarkerInGoal(t *testing.T) {
	m := NewManager(&config.Config{APIKey: "test"}, "/tmp", false)
	_, err := m.Spawn(t.Context(), "[celeste-subagent] do something", "")
	if err == nil {
		t.Fatal("expected error for recursion marker in goal")
	}
	if err.Error() != "recursive subagent spawning detected" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetRunAndListRuns(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)

	// Manually add runs for testing
	m.mu.Lock()
	m.runs["run-1"] = &SubagentRun{ID: "run-1", Status: "completed"}
	m.runs["run-2"] = &SubagentRun{ID: "run-2", Status: "failed"}
	m.mu.Unlock()

	run, ok := m.GetRun("run-1")
	if !ok {
		t.Fatal("expected to find run-1")
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed, got %s", run.Status)
	}

	_, ok = m.GetRun("nonexistent")
	if ok {
		t.Fatal("should not find nonexistent run")
	}

	runs := m.ListRuns()
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
}

func TestIDUniqueness(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)

	// Generate IDs by incrementing counter
	m.mu.Lock()
	m.counter++
	id1 := m.counter
	m.counter++
	id2 := m.counter
	m.mu.Unlock()

	if id1 == id2 {
		t.Fatal("IDs should be unique")
	}
}

// TestSubagentRun_CarriesCheckpointID verifies that CheckpointID round-trips
// on SubagentRun without a live LLM. A full spawn→fail→resume integration test
// would require a mock agent runner; this structural test confirms the field
// is wired and tagged correctly.
func TestSubagentRun_CarriesCheckpointID(t *testing.T) {
	run := &SubagentRun{ID: "fire-1", CheckpointID: "run_abc"}
	if run.CheckpointID != "run_abc" {
		t.Fatal("checkpoint id not carried on SubagentRun")
	}
}

// TestSubagentRun_CheckpointIDOnFailedRun verifies that a failed run
// populated via manager state tracking retains its CheckpointID.
func TestSubagentRun_CheckpointIDOnFailedRun(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	m.mu.Lock()
	run := &SubagentRun{
		ID:           "sub-failed",
		Status:       "failed",
		CheckpointID: "20240101-120000.000000000",
		Error:        "timeout",
	}
	m.runs["sub-failed"] = run
	m.mu.Unlock()

	got, ok := m.GetRun("sub-failed")
	if !ok {
		t.Fatal("run not found")
	}
	if got.CheckpointID != "20240101-120000.000000000" {
		t.Fatalf("CheckpointID = %q, want 20240101-120000.000000000", got.CheckpointID)
	}
}

// fakeExecFor returns an execFunc that sleeps for the given duration, then
// marks the run as "completed" under m.mu and returns it.
// The mutex must be held when writing run fields to match the contract of
// executeSubagent (which always locks before writing Status/Result/EndedAt).
func fakeExecFor(m *Manager, sleep time.Duration) execFunc {
	return func(_ context.Context, run *SubagentRun, _, _ string, _ TurnCallback, _ int, _ bool) (*SubagentRun, error) {
		time.Sleep(sleep)
		m.mu.Lock()
		run.Status = "completed"
		run.Result = "fake result"
		run.EndedAt = time.Now()
		m.mu.Unlock()
		return run, nil
	}
}

// TestSpawnAsync_StructuralReturns verifies that SpawnAsync returns a buffered
// channel (cap 1) and a non-nil run handle. The initial status is verified
// under m.mu to avoid a race with the exec goroutine.
func TestSpawnAsync_StructuralReturns(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	// Use a small sleep so the goroutine hasn't finished by the time we check.
	m.execFn = fakeExecFor(m, 50*time.Millisecond)

	ch, run := m.SpawnAsync(context.Background(), "do stuff", "/tmp", SpawnOptions{})
	if ch == nil {
		t.Fatal("SpawnAsync returned nil channel")
	}
	if cap(ch) != 1 {
		t.Fatalf("channel capacity = %d, want 1 (buffered)", cap(ch))
	}
	if run == nil {
		t.Fatal("SpawnAsync returned nil run")
	}
	// Read status under m.mu to avoid racing with the exec goroutine.
	m.mu.Lock()
	status := run.Status
	m.mu.Unlock()
	if status != "running" {
		t.Fatalf("run.Status = %q, want \"running\"", status)
	}
	// Drain to let the goroutine finish cleanly.
	<-ch
}

// TestSpawnAsync_ChannelDeliversCompletion verifies the channel eventually
// receives a completed run.
func TestSpawnAsync_ChannelDeliversCompletion(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	m.execFn = fakeExecFor(m, 5*time.Millisecond)

	ch, _ := m.SpawnAsync(context.Background(), "do stuff", "/tmp", SpawnOptions{})
	select {
	case final := <-ch:
		if final.Status != "completed" {
			t.Fatalf("final.Status = %q, want \"completed\"", final.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SpawnAsync to deliver result")
	}
}

// TestSpawnWithOptions_DefaultBlocking verifies that with BackgroundAfter==0
// SpawnWithOptions blocks until completion (recursion guard path aside).
// Uses the recursion marker rejection to exercise the early-return path without
// a live LLM; the default blocking path is exercised via fakeExec below.
func TestSpawnWithOptions_DefaultBlocking_FakeExec(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	m.execFn = fakeExecFor(m, 5*time.Millisecond)

	run, err := m.SpawnWithOptions(context.Background(), "some goal", "/tmp", SpawnOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run == nil {
		t.Fatal("SpawnWithOptions returned nil run")
	}
	if run.Status != "completed" {
		t.Fatalf("run.Status = %q, want \"completed\" (blocking path)", run.Status)
	}
}

// TestSpawnWithOptions_FastTask_NoBackground verifies that when a task
// finishes before the BackgroundAfter threshold the caller gets a completed
// run (not a "background" run).
func TestSpawnWithOptions_FastTask_NoBackground(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	// Task finishes in 5ms; threshold is 100ms — should never background.
	m.execFn = fakeExecFor(m, 5*time.Millisecond)

	run, err := m.SpawnWithOptions(context.Background(), "fast goal", "/tmp", SpawnOptions{
		BackgroundAfter: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("run.Status = %q, want \"completed\" (finished before threshold)", run.Status)
	}
}

// TestSpawnWithOptions_SlowTask_TransitionsToBackground verifies that when a
// task exceeds BackgroundAfter the returned run has status "background" and
// later transitions to "completed" via the watcher goroutine, triggering
// OnBackgroundComplete.
func TestSpawnWithOptions_SlowTask_TransitionsToBackground(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	// Task takes 200ms; threshold is 10ms — should background quickly.
	m.execFn = fakeExecFor(m, 200*time.Millisecond)

	var callbackFired atomic.Bool
	var callbackRun *SubagentRun
	m.OnBackgroundComplete = func(r *SubagentRun) {
		callbackRun = r
		callbackFired.Store(true)
	}

	run, err := m.SpawnWithOptions(context.Background(), "slow goal", "/tmp", SpawnOptions{
		BackgroundAfter: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SpawnWithOptions should have returned while the task is still running.
	// Read status under m.mu to avoid racing with the exec goroutine.
	m.mu.Lock()
	statusAfterReturn := run.Status
	m.mu.Unlock()
	if statusAfterReturn != "background" {
		t.Fatalf("run.Status = %q, want \"background\" immediately after threshold", statusAfterReturn)
	}

	// Wait for the watcher goroutine to finish and the callback to fire.
	deadline := time.After(3 * time.Second)
	for !callbackFired.Load() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for OnBackgroundComplete to fire")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// After callback fires, the run should reflect "completed".
	m.mu.Lock()
	finalStatus := run.Status
	m.mu.Unlock()
	if finalStatus != "completed" {
		t.Fatalf("run.Status = %q after watcher; want \"completed\"", finalStatus)
	}

	// Callback received the same run pointer.
	if callbackRun != run {
		t.Fatal("OnBackgroundComplete received different run pointer than returned")
	}

	// ListRuns should reflect the completed state.
	runs := m.ListRuns()
	found := false
	for _, r := range runs {
		if r.ID == run.ID && r.Status == "completed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ListRuns did not reflect completed background run")
	}
}

// TestSpawnWithOptions_NoCallbackOnNilOnBackgroundComplete verifies that a nil
// OnBackgroundComplete does not panic when a background task completes.
func TestSpawnWithOptions_NoCallbackOnNilOnBackgroundComplete(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	m.execFn = fakeExecFor(m, 200*time.Millisecond)
	// OnBackgroundComplete is nil (default) — must not panic.

	run, err := m.SpawnWithOptions(context.Background(), "slow goal", "/tmp", SpawnOptions{
		BackgroundAfter: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Read status under m.mu to avoid racing with the exec goroutine.
	m.mu.Lock()
	statusAfterReturn := run.Status
	m.mu.Unlock()
	if statusAfterReturn != "background" {
		t.Fatalf("run.Status = %q, want \"background\"", statusAfterReturn)
	}

	// Give watcher goroutine time to complete without panicking.
	time.Sleep(350 * time.Millisecond)

	m.mu.Lock()
	finalStatus := run.Status
	m.mu.Unlock()
	if finalStatus != "completed" {
		t.Fatalf("run.Status = %q after watcher; want \"completed\"", finalStatus)
	}
}
