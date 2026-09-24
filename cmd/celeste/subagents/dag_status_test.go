package subagents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// scriptedExec fails any run whose goal contains "fail" and completes the rest,
// recording every goal it was asked to run.
type scriptedExec struct {
	mu    sync.Mutex
	goals []string
}

func (s *scriptedExec) fn(m *Manager) execFunc {
	return func(_ context.Context, run *SubagentRun, goal, _ string, _ TurnCallback, _ int, _ bool) (*SubagentRun, error) {
		s.mu.Lock()
		s.goals = append(s.goals, goal)
		s.mu.Unlock()
		m.mu.Lock()
		if strings.Contains(goal, "fail") {
			run.Status = "failed"
			run.Error = "scripted failure"
		} else {
			run.Status = "completed"
			run.Result = "done"
		}
		run.EndedAt = time.Now()
		m.mu.Unlock()
		return run, nil
	}
}

func (s *scriptedExec) ran(substr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.goals {
		if strings.Contains(g, substr) {
			return true
		}
	}
	return false
}

func runStatus(m *Manager, run *SubagentRun) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return run.Status
}

func waitForStatus(t *testing.T, m *Manager, id, want string) *SubagentRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if run, ok := m.GetRun(id); ok && runStatus(m, run) == want {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %q never reached status %q", id, want)
	return nil
}

type spawnResult struct {
	run *SubagentRun
	err error
}

func spawnInBackground(ctx context.Context, m *Manager, goal string, opts SpawnOptions) <-chan spawnResult {
	ch := make(chan spawnResult, 1)
	go func() {
		run, err := m.SpawnWithOptions(ctx, goal, "/tmp", opts)
		ch <- spawnResult{run, err}
	}()
	return ch
}

func awaitSpawn(t *testing.T, ch <-chan spawnResult) spawnResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("SpawnWithOptions did not return; a waiting run was left blocked")
		return spawnResult{}
	}
}

// A failed dependency fails its dependents, and theirs, promptly (#171).
// Before, they sat in "waiting" until the caller's context expired.
func TestDAG_FailedDependencyCascades(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	exec := &scriptedExec{}
	m.execFn = exec.fn(m)

	b := spawnInBackground(context.Background(), m, "build on a", SpawnOptions{TaskID: "b", DependsOn: []string{"a"}})
	waitForStatus(t, m, "b", "waiting")
	c := spawnInBackground(context.Background(), m, "build on b", SpawnOptions{TaskID: "c", DependsOn: []string{"b"}})
	waitForStatus(t, m, "c", "waiting")

	if _, err := m.SpawnWithOptions(context.Background(), "this will fail", "/tmp", SpawnOptions{TaskID: "a"}); err != nil {
		t.Fatalf("spawn a: %v", err)
	}

	for name, ch := range map[string]<-chan spawnResult{"b": b, "c": c} {
		r := awaitSpawn(t, ch)
		if got := runStatus(m, r.run); got != "failed" {
			t.Errorf("%s: status %q, want failed", name, got)
		}
		if !strings.Contains(r.run.Error, "dependency") {
			t.Errorf("%s: error %q should name the failed dependency", name, r.run.Error)
		}
	}
	if exec.ran("build on") {
		t.Error("a dependent of a failed run was executed")
	}
}

// Spawning against an already-failed dependency fails immediately.
func TestDAG_SpawnAfterDependencyFailed(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	exec := &scriptedExec{}
	m.execFn = exec.fn(m)

	if _, err := m.SpawnWithOptions(context.Background(), "this will fail", "/tmp", SpawnOptions{TaskID: "a"}); err != nil {
		t.Fatalf("spawn a: %v", err)
	}
	r := awaitSpawn(t, spawnInBackground(context.Background(), m, "build on a", SpawnOptions{TaskID: "b", DependsOn: []string{"a"}}))
	if r.err == nil || runStatus(m, r.run) != "failed" {
		t.Fatalf("want an immediate failure, got status %q err %v", runStatus(m, r.run), r.err)
	}
}

// Kill reaches a run that is waiting on dependencies, releases its caller, and
// the run never starts later (#171).
func TestDAG_KillWaitingRun(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	exec := &scriptedExec{}
	m.execFn = exec.fn(m)

	b := spawnInBackground(context.Background(), m, "build on a", SpawnOptions{TaskID: "b", DependsOn: []string{"a"}})
	waitForStatus(t, m, "b", "waiting")

	if !m.Kill("b") {
		t.Fatal("Kill returned false for a waiting run")
	}
	r := awaitSpawn(t, b)
	if got := runStatus(m, r.run); got != "failed" {
		t.Fatalf("killed waiting run has status %q, want failed", got)
	}

	// Completing the dependency afterwards must not start the killed run.
	if _, err := m.SpawnWithOptions(context.Background(), "produce a", "/tmp", SpawnOptions{TaskID: "a"}); err != nil {
		t.Fatalf("spawn a: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if exec.ran("build on a") {
		t.Error("a killed waiting run was executed once its dependency completed")
	}
}

// A run whose caller stopped waiting is dropped instead of starting later with
// nobody to return to (#171).
func TestDAG_AbandonedWaiterDoesNotRun(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	exec := &scriptedExec{}
	m.execFn = exec.fn(m)

	ctx, cancel := context.WithCancel(context.Background())
	b := spawnInBackground(ctx, m, "build on a", SpawnOptions{TaskID: "b", DependsOn: []string{"a"}})
	waitForStatus(t, m, "b", "waiting")
	cancel()
	r := awaitSpawn(t, b)
	if got := runStatus(m, r.run); got != "failed" {
		t.Fatalf("abandoned run has status %q, want failed", got)
	}

	if _, err := m.SpawnWithOptions(context.Background(), "produce a", "/tmp", SpawnOptions{TaskID: "a"}); err != nil {
		t.Fatalf("spawn a: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if exec.ran("build on a") {
		t.Error("an abandoned waiting run was executed once its dependency completed")
	}
}

// A completed dependency still starts its dependent, with the result attached.
func TestDAG_CompletedDependencyStartsDependent(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	exec := &scriptedExec{}
	m.execFn = exec.fn(m)

	b := spawnInBackground(context.Background(), m, "build on a", SpawnOptions{TaskID: "b", DependsOn: []string{"a"}})
	waitForStatus(t, m, "b", "waiting")
	if _, err := m.SpawnWithOptions(context.Background(), "produce a", "/tmp", SpawnOptions{TaskID: "a"}); err != nil {
		t.Fatalf("spawn a: %v", err)
	}
	r := awaitSpawn(t, b)
	if got := runStatus(m, r.run); got != "completed" {
		t.Fatalf("dependent status %q, want completed", got)
	}
	if !exec.ran("[DEPENDENCY RESULT") {
		t.Error("dependent goal was not prefixed with its dependency's result")
	}
}

// RunGoal returns a nil error when it stops at max turns or for lack of
// progress; those runs are not "completed" (#171).
func TestIncompleteRunError(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{agent.StatusCompleted, false},
		{agent.StatusMaxTurnsReached, true},
		{agent.StatusNoProgressStopped, true},
		{agent.StatusCancelled, true},
	}
	for _, tc := range cases {
		got := incompleteRunError(&agent.RunState{Status: tc.status, Turn: 7})
		if (got != "") != tc.want {
			t.Errorf("status %q: incompleteRunError = %q, want failure=%v", tc.status, got, tc.want)
		}
		if tc.want && !strings.Contains(got, tc.status) {
			t.Errorf("status %q: message %q should name the status", tc.status, got)
		}
	}
	if got := incompleteRunError(nil); got != "" {
		t.Errorf("nil state: got %q, want empty", got)
	}
}

func writeSliders(t *testing.T, cfg *config.SliderConfig) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".celeste"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save sliders: %v", err)
	}
}

// The model can turn R18 off for a subagent but never on, whether directly or
// through a preset; enabling it is the user's decision (#171).
func TestSliderOverrideCannotEnableR18(t *testing.T) {
	const eligibility = "Content Eligibility"

	user := config.DefaultSliderConfig()
	user.R18Enabled = false
	user.Presets["spicy"] = config.SliderPreset{Lewdness: 10, R18Enabled: true}
	writeSliders(t, user)

	if got := buildSliderOverride(map[string]any{"r18": true, "lewdness": 10.0}); strings.Contains(got, eligibility) {
		t.Error("persona r18=true enabled R18 when the user has it off")
	}
	if got := buildSliderOverride(map[string]any{"preset": "spicy"}); strings.Contains(got, eligibility) {
		t.Error("an R18 preset enabled R18 when the user has it off")
	}

	user.R18Enabled = true
	user.Lewdness = 10
	writeSliders(t, user)
	if got := buildSliderOverride(map[string]any{"r18": false}); strings.Contains(got, eligibility) {
		t.Error("persona r18=false did not disable R18")
	}
	if got := buildSliderOverride(map[string]any{"lewdness": 10.0}); !strings.Contains(got, eligibility) {
		t.Error("user-enabled R18 should still apply when the model doesn't override it")
	}
}
