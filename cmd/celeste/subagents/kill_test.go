package subagents

import (
	"context"
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

// Kill on an unknown id returns false rather than panicking.
func TestKill_UnknownIDReturnsFalse(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	if m.Kill("does-not-exist") {
		t.Fatal("Kill should return false for an unknown id")
	}
}
