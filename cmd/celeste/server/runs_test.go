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

// completeRun's own eviction pass (added to enforce the cap without waiting
// on the next registration) must not be able to evict the very run it just
// completed. If every other tracked run is still "running", the run that
// just completed is the ONLY eviction-eligible entry — an unprotected pass
// deletes it immediately, before a client could ever poll for the result.
func TestCompleteRunDoesNotEvictItself(t *testing.T) {
	s := New(Config{})

	// Fill past capacity with still-running runs, so once the first of them
	// completes, evictLocked's "oldest completed" search has exactly one
	// candidate: the run that just completed.
	cancels := make([]context.CancelFunc, 0, maxTrackedRuns+1)
	for i := 0; i < maxTrackedRuns+1; i++ {
		id := fmt.Sprintf("bg-run-%03d", i)
		_, c := context.WithCancel(context.Background())
		cancels = append(cancels, c)
		s.registerRun(id, c)
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	s.completeRun("bg-run-000", &BackgroundRun{Status: "completed", Result: "keep me"})

	got, ok := s.lookupRun("bg-run-000")
	if !ok {
		t.Fatal("the run that just completed was evicted in the same call that completed it — a client could never have polled it")
	}
	if got.Status != "completed" || got.Result != "keep me" {
		t.Errorf("run = %+v, want the completed state intact", got)
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

// Finding 4: nothing asserted that a terminal run is no longer cancellable.
// completeRun sets run.cancel = nil; this is the test that fails if that line
// is deleted.
func TestCancelRunReturnsFalseForCompletedRun(t *testing.T) {
	s := New(Config{})
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.registerRun("bg-done", cancel)
	s.completeRun("bg-done", &BackgroundRun{Status: "completed", Result: "ok"})

	if s.cancelRun("bg-done") {
		t.Error("cancelRun returned true for an already-completed run")
	}
	got, ok := s.lookupRun("bg-done")
	if !ok {
		t.Fatal("run vanished")
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want it to remain completed after a no-op cancel attempt", got.Status)
	}
}

// Finding 4: nothing asserted that the run's context is actually cancelled
// once the run reaches a terminal state — only that cancelRun itself cancels
// it (TestCancelRun above). This exercises the watcher goroutine's own
// cancel() call after a normal (non-cancelled) completion in handlers.go.
func TestBackgroundRunCancelsContextOnCompletion(t *testing.T) {
	origAfter := backgroundAfter
	origExec := agentExecFn
	t.Cleanup(func() { backgroundAfter = origAfter; agentExecFn = origExec })
	backgroundAfter = 50 * time.Millisecond

	s := New(Config{})
	release := make(chan struct{})
	var capturedCtx context.Context
	agentExecFn = func(ctx context.Context, cfg *config.Config, goal, workspace string) (agentOutcome, error) {
		capturedCtx = ctx
		<-release
		return agentOutcome{Text: "done"}, nil
	}

	blocks, err := s.runAgentMode(context.Background(), &config.Config{}, "go", t.TempDir())
	if err != nil {
		t.Fatalf("runAgentMode: %v", err)
	}
	var handle struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(blocks[0].Text), &handle); err != nil {
		t.Fatalf("handle is not JSON: %v (%s)", err, blocks[0].Text)
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := s.lookupRun(handle.RunID); ok && got.Status == "completed" {
			select {
			case <-capturedCtx.Done():
			case <-time.After(time.Second):
				t.Error("run context was not cancelled after completion")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run never reached completed")
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

// Finding 1 regression: cancelling a background run must leave its status
// "cancelled" forever after — the watcher goroutine's completeRun call must
// not overwrite it with "failed" once the exec goroutine unwinds with
// ctx.Err(). This is not a race: cancelRun always sets Status=cancelled and
// cancels the context BEFORE agentExecFn can observe cancellation and
// return, so completeRun is guaranteed to run after cancelRun's write on
// every successful cancel, not just sometimes.
func TestCancelledRunStaysCancelled(t *testing.T) {
	origAfter := backgroundAfter
	origExec := agentExecFn
	t.Cleanup(func() { backgroundAfter = origAfter; agentExecFn = origExec })
	backgroundAfter = 50 * time.Millisecond

	s := New(Config{})
	agentExecFn = func(ctx context.Context, cfg *config.Config, goal, workspace string) (agentOutcome, error) {
		<-ctx.Done()
		return agentOutcome{}, fmt.Errorf("run cancelled: %w", ctx.Err())
	}

	blocks, err := s.runAgentMode(context.Background(), &config.Config{}, "go", t.TempDir())
	if err != nil {
		t.Fatalf("runAgentMode: %v", err)
	}
	var handle struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(blocks[0].Text), &handle); err != nil {
		t.Fatalf("handle is not JSON: %v (%s)", err, blocks[0].Text)
	}

	if !s.cancelRun(handle.RunID) {
		t.Fatal("cancelRun returned false for a live run")
	}
	if got, ok := s.lookupRun(handle.RunID); !ok || got.Status != "cancelled" {
		t.Fatalf("immediately after cancelRun: run = %+v, ok = %v, want status cancelled", got, ok)
	}

	// Give the exec goroutine time to observe ctx.Done(), return its
	// cancellation error, and let the watcher goroutine call completeRun with
	// it. Every poll in this window must still say cancelled.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := s.lookupRun(handle.RunID)
		if !ok {
			t.Fatal("run vanished after cancellation")
		}
		if got.Status != "cancelled" {
			t.Fatalf("status became %q after cancellation; want it to stay cancelled forever, not be overwritten by the watcher's completeRun", got.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Finding 2 regression: a panic inside agentExecFn, on a background run's
// exec goroutine, must not take the whole process down with it — it should
// surface as that run's own "failed" status instead. The panic fires only
// after the run has already been registered as background (release is
// closed after runAgentMode returns the handle), so this exercises the
// watcher-goroutine recovery path specifically, not the fast inline path.
// Reaching the end of this test at all is itself part of the proof: an
// unrecovered panic on this goroutine would crash the whole test binary,
// not just fail an assertion.
func TestBackgroundRunPanicRecovered(t *testing.T) {
	origAfter := backgroundAfter
	origExec := agentExecFn
	t.Cleanup(func() { backgroundAfter = origAfter; agentExecFn = origExec })
	backgroundAfter = 50 * time.Millisecond

	s := New(Config{})
	release := make(chan struct{})
	agentExecFn = func(ctx context.Context, cfg *config.Config, goal, workspace string) (agentOutcome, error) {
		<-release
		panic("boom: simulated agent panic")
	}

	blocks, err := s.runAgentMode(context.Background(), &config.Config{}, "go", t.TempDir())
	if err != nil {
		t.Fatalf("runAgentMode: %v", err)
	}
	var handle struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(blocks[0].Text), &handle); err != nil {
		t.Fatalf("handle is not JSON: %v (%s)", err, blocks[0].Text)
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := s.lookupRun(handle.RunID); ok && got.Status == "failed" {
			if !strings.Contains(got.Error, "panic") {
				t.Errorf("Error = %q, want it to name the panic", got.Error)
			}
			if !strings.Contains(got.Error, "boom") {
				t.Errorf("Error = %q, want it to carry the recovered value", got.Error)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("panicked run never reached a terminal state — recover may not be sending on resultCh, which would hang the watcher forever")
}

// The no-argument payload must not change: existing callers (the
// celeste-for-claude skills, any client) depend on these keys — the same
// set, not a superset or subset. New(Config{}) leaves CelesteConfig nil, so
// "provider" and "model" (which are conditional on it) are correctly absent
// here; "workspace" and "transport" are unconditional and must be present.
func TestCelesteStatusNoArgsUnchanged(t *testing.T) {
	s := New(Config{})
	registerCelesteStatusTool(s)

	blocks, err := s.handlers["celeste_status"](context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("celeste_status: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(blocks[0].Text), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}

	want := map[string]bool{
		"server": true, "version": true, "commit": true,
		"uptime": true, "health": true, "workspace": true, "transport": true,
	}
	for k := range payload {
		if !want[k] {
			t.Errorf("no-arg payload has unexpected key %q", k)
			continue
		}
		delete(want, k)
	}
	for k := range want {
		t.Errorf("no-arg payload is missing key %q", k)
	}

	if _, ok := payload["run_id"]; ok {
		t.Error("no-arg payload must not carry run_id")
	}
}

func TestCelesteStatusReportsRun(t *testing.T) {
	s := New(Config{})
	registerCelesteStatusTool(s)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.registerRun("bg-status", cancel)

	blocks, err := s.handlers["celeste_status"](context.Background(), map[string]any{"run_id": "bg-status"})
	if err != nil {
		t.Fatalf("celeste_status: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(blocks[0].Text), &payload)
	if payload["run_id"] != "bg-status" {
		t.Errorf("run_id = %v, want bg-status", payload["run_id"])
	}
	if payload["status"] != "running" {
		t.Errorf("status = %v, want running", payload["status"])
	}
	if _, ok := payload["elapsed"]; !ok {
		t.Error("a running run should report elapsed so a caller can see it is alive")
	}
}

// A client restart kills the server and every run with it. An unknown id must
// say so, or someone hunts for a bug that is not there.
func TestCelesteStatusUnknownRunNamesRestart(t *testing.T) {
	s := New(Config{})
	registerCelesteStatusTool(s)

	blocks, err := s.handlers["celeste_status"](context.Background(), map[string]any{"run_id": "bg-gone"})
	if err != nil {
		t.Fatalf("celeste_status: %v", err)
	}
	if !strings.Contains(strings.ToLower(blocks[0].Text), "restart") {
		t.Errorf("unknown-run message should explain the restart case, got: %s", blocks[0].Text)
	}
}

func TestCelesteStatusCancels(t *testing.T) {
	s := New(Config{})
	registerCelesteStatusTool(s)
	ctx, cancel := context.WithCancel(context.Background())
	s.registerRun("bg-cancel", cancel)

	blocks, err := s.handlers["celeste_status"](context.Background(),
		map[string]any{"run_id": "bg-cancel", "cancel": true})
	if err != nil {
		t.Fatalf("celeste_status: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(blocks[0].Text), &payload)
	if payload["status"] != "cancelled" {
		t.Errorf("status = %v, want cancelled", payload["status"])
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Error("cancel did not cancel the run context")
	}
}

// Finding 4: only a *running* run's celeste_status payload was exercised
// before this. A terminal run's payload must carry its result, agent_run_id,
// and a sane completed-elapsed value (EndedAt - StartedAt, not time-since-now).
func TestCelesteStatusReportsCompletedRun(t *testing.T) {
	s := New(Config{})
	registerCelesteStatusTool(s)
	_, cancel := context.WithCancel(context.Background())
	s.registerRun("bg-done-status", cancel)
	s.completeRun("bg-done-status", &BackgroundRun{
		Status:     "completed",
		Result:     "the answer",
		AgentRunID: "20260809-999",
		Turns:      3,
		ToolCalls:  5,
	})

	blocks, err := s.handlers["celeste_status"](context.Background(), map[string]any{"run_id": "bg-done-status"})
	if err != nil {
		t.Fatalf("celeste_status: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(blocks[0].Text), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload["status"] != "completed" {
		t.Errorf("status = %v, want completed", payload["status"])
	}
	if payload["result"] != "the answer" {
		t.Errorf("result = %v, want %q", payload["result"], "the answer")
	}
	if payload["agent_run_id"] != "20260809-999" {
		t.Errorf("agent_run_id = %v, want it recorded so `celeste agent -resume` stays usable", payload["agent_run_id"])
	}
	elapsed, _ := payload["elapsed"].(string)
	if elapsed == "" {
		t.Fatal("a completed run must report elapsed")
	}
	if d, err := time.ParseDuration(elapsed); err != nil || d < 0 {
		t.Errorf("elapsed = %q, want a sane non-negative duration (EndedAt - StartedAt), got parse error %v", elapsed, err)
	}
}

// Finding 4 / Finding 1: cancel:true against an already-terminal run is
// exactly the path Finding 1 lived on. It must be a no-op — the status must
// stay whatever terminal state it already reached, not flip to cancelled.
func TestCelesteStatusCancelAlreadyTerminalIsNoop(t *testing.T) {
	s := New(Config{})
	registerCelesteStatusTool(s)
	_, cancel := context.WithCancel(context.Background())
	s.registerRun("bg-term", cancel)
	s.completeRun("bg-term", &BackgroundRun{Status: "completed", Result: "already done"})

	blocks, err := s.handlers["celeste_status"](context.Background(),
		map[string]any{"run_id": "bg-term", "cancel": true})
	if err != nil {
		t.Fatalf("celeste_status: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(blocks[0].Text), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload["status"] != "completed" {
		t.Errorf("status = %v, want completed — cancelling an already-terminal run must be a no-op, not flip it to cancelled", payload["status"])
	}
}
