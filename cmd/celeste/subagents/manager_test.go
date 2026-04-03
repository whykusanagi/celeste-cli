package subagents

import (
	"testing"

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
