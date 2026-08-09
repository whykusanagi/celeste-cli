package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func TestRegisterAndLookupRun(t *testing.T) {
	s := New(Config{})
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	run := s.registerRun("bg-1", cancel)
	if run == nil {
		t.Fatal("registerRun returned nil")
	}
	if run.Status != "running" {
		t.Errorf("Status = %q, want %q", run.Status, "running")
	}

	got, ok := s.lookupRun("bg-1")
	if !ok {
		t.Fatal("lookupRun did not find a registered run")
	}
	if got.ID != "bg-1" {
		t.Errorf("ID = %q, want %q", got.ID, "bg-1")
	}

	if _, ok := s.lookupRun("bg-missing"); ok {
		t.Error("lookupRun found an unregistered run")
	}
}

func TestCompleteRunStoresTerminalState(t *testing.T) {
	s := New(Config{})
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.registerRun("bg-2", cancel)

	s.completeRun("bg-2", &BackgroundRun{
		Status:     "completed",
		Result:     "the answer",
		AgentRunID: "20260809-120000.000000000-1",
		Turns:      4,
		ToolCalls:  6,
	})

	got, ok := s.lookupRun("bg-2")
	if !ok {
		t.Fatal("run vanished after completion")
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want completed", got.Status)
	}
	if got.Result != "the answer" {
		t.Errorf("Result = %q, want %q", got.Result, "the answer")
	}
	if got.AgentRunID == "" {
		t.Error("AgentRunID must be recorded so `celeste agent -resume` stays usable")
	}
	if got.EndedAt.IsZero() {
		t.Error("EndedAt must be stamped on completion")
	}
}

// A caller polling after completion must still get its result, so completed runs
// are retained — which means the map needs a bound or it grows for the life of
// the server. Running runs must never be evicted.
func TestRegistryEvictsOldestCompletedNeverRunning(t *testing.T) {
	s := New(Config{})

	// One long-lived running entry, registered first so it is the oldest.
	_, cancelLive := context.WithCancel(context.Background())
	defer cancelLive()
	s.registerRun("bg-live", cancelLive)

	// Fill well past the cap with completed runs.
	for i := 0; i < maxTrackedRuns+10; i++ {
		id := fmt.Sprintf("bg-%03d", i)
		_, c := context.WithCancel(context.Background())
		s.registerRun(id, c)
		s.completeRun(id, &BackgroundRun{Status: "completed", Result: "x"})
		c()
	}

	if _, ok := s.lookupRun("bg-live"); !ok {
		t.Error("a running run was evicted; only completed runs may be evicted")
	}
	if n := s.runCount(); n > maxTrackedRuns {
		t.Errorf("registry holds %d runs, want at most %d", n, maxTrackedRuns)
	}
	if _, ok := s.lookupRun("bg-000"); ok {
		t.Error("the oldest completed run should have been evicted first")
	}
}

func TestCancelRun(t *testing.T) {
	s := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	s.registerRun("bg-3", cancel)

	if !s.cancelRun("bg-3") {
		t.Fatal("cancelRun returned false for a live run")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Error("cancelRun did not cancel the run's context")
	}
	if s.cancelRun("bg-missing") {
		t.Error("cancelRun returned true for an unknown run")
	}
}

// The registry is reached from request handlers and watcher goroutines at once.
// This exists to be run under -race.
func TestRegistryConcurrentAccess(t *testing.T) {
	s := New(Config{})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("bg-c%02d", i)
			_, c := context.WithCancel(context.Background())
			s.registerRun(id, c)
			s.lookupRun(id)
			s.completeRun(id, &BackgroundRun{Status: "completed"})
			s.lookupRun(id)
			c()
		}(i)
	}
	wg.Wait()
}

func TestNewRunIDIsUniqueAndPrefixed(t *testing.T) {
	a := newRunID(time.Unix(0, 1))
	b := newRunID(time.Unix(0, 2))
	if a == b {
		t.Error("two run IDs collided")
	}
	if len(a) < 4 || a[:3] != "bg-" {
		t.Errorf("run ID %q should start with bg-", a)
	}
}

// Under the threshold the response must be exactly what it is today — the
// result inline, no handle, nothing registered. Over it, a handle whose status
// is running, which later reports completed. Driven by a fake slow run rather
// than a live model so the threshold logic is deterministic.
func TestBackgroundThreshold(t *testing.T) {
	origAfter := backgroundAfter
	origExec := agentExecFn
	t.Cleanup(func() { backgroundAfter = origAfter; agentExecFn = origExec })

	backgroundAfter = 50 * time.Millisecond

	t.Run("under threshold returns inline", func(t *testing.T) {
		s := New(Config{})
		agentExecFn = func(ctx context.Context, cfg *config.Config, goal, workspace string) (agentOutcome, error) {
			return agentOutcome{Text: "fast answer", Turns: 1}, nil
		}
		blocks, err := s.runAgentMode(context.Background(), &config.Config{}, "go", t.TempDir())
		if err != nil {
			t.Fatalf("runAgentMode: %v", err)
		}
		if len(blocks) == 0 || !strings.Contains(blocks[0].Text, "fast answer") {
			t.Errorf("expected the result inline, got %+v", blocks)
		}
		if n := s.runCount(); n != 0 {
			t.Errorf("registry holds %d runs, want 0 — a fast run must not register", n)
		}
	})

	t.Run("over threshold returns a handle then completes", func(t *testing.T) {
		s := New(Config{})
		release := make(chan struct{})
		agentExecFn = func(ctx context.Context, cfg *config.Config, goal, workspace string) (agentOutcome, error) {
			<-release
			return agentOutcome{Text: "slow answer", Turns: 4, ToolCalls: 6, AgentRunID: "20260809-1"}, nil
		}

		blocks, err := s.runAgentMode(context.Background(), &config.Config{}, "go", t.TempDir())
		if err != nil {
			t.Fatalf("runAgentMode: %v", err)
		}
		var handle struct {
			RunID  string `json:"run_id"`
			Status string `json:"status"`
			Poll   string `json:"poll"`
		}
		if err := json.Unmarshal([]byte(blocks[0].Text), &handle); err != nil {
			t.Fatalf("handle is not JSON: %v (%s)", err, blocks[0].Text)
		}
		if handle.Status != "running" {
			t.Errorf("status = %q, want running", handle.Status)
		}
		if handle.RunID == "" {
			t.Fatal("handle carries no run_id")
		}
		if !strings.Contains(handle.Poll, handle.RunID) {
			t.Errorf("poll hint %q should name the run_id so a model need not infer it", handle.Poll)
		}

		close(release)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if got, ok := s.lookupRun(handle.RunID); ok && got.Status == "completed" {
				if got.Result != "slow answer" {
					t.Errorf("Result = %q, want %q", got.Result, "slow answer")
				}
				if got.AgentRunID != "20260809-1" {
					t.Errorf("AgentRunID = %q, want it recorded", got.AgentRunID)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("run never reached completed")
	})

	t.Run("over threshold failure is recorded", func(t *testing.T) {
		s := New(Config{})
		release := make(chan struct{})
		agentExecFn = func(ctx context.Context, cfg *config.Config, goal, workspace string) (agentOutcome, error) {
			<-release
			return agentOutcome{}, errors.New("boom")
		}
		blocks, err := s.runAgentMode(context.Background(), &config.Config{}, "go", t.TempDir())
		if err != nil {
			t.Fatalf("runAgentMode: %v", err)
		}
		var handle struct {
			RunID string `json:"run_id"`
		}
		_ = json.Unmarshal([]byte(blocks[0].Text), &handle)
		close(release)

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if got, ok := s.lookupRun(handle.RunID); ok && got.Status == "failed" {
				if !strings.Contains(got.Error, "boom") {
					t.Errorf("Error = %q, want it to carry the failure", got.Error)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("failed run never reached a terminal state")
	})
}
