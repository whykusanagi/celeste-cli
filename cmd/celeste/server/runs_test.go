package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
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
