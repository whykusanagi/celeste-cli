package subagents

import (
	"encoding/json"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestSpawnAgentToolName(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewSpawnAgentTool(m)
	if tool.Name() != "spawn_agent" {
		t.Fatalf("expected name 'spawn_agent', got %q", tool.Name())
	}
}

func TestSpawnAgentToolValidateInput(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewSpawnAgentTool(m)

	// Missing goal
	err := tool.ValidateInput(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing goal")
	}

	// Empty goal
	err = tool.ValidateInput(map[string]any{"goal": ""})
	if err == nil {
		t.Fatal("expected error for empty goal")
	}

	// Valid goal
	err = tool.ValidateInput(map[string]any{"goal": "do something"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpawnAgentToolParameters(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewSpawnAgentTool(m)

	params := tool.Parameters()
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("expected type 'object', got %v", schema["type"])
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["goal"]; !ok {
		t.Fatal("schema missing 'goal' property")
	}
	if _, ok := props["task_id"]; !ok {
		t.Fatal("schema missing 'task_id' property (DAG orchestration)")
	}
	if _, ok := props["depends_on"]; !ok {
		t.Fatal("schema missing 'depends_on' property (DAG orchestration)")
	}
	if _, ok := props["persona"]; !ok {
		t.Fatal("schema missing 'persona' property")
	}
	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "goal" {
		t.Fatalf("expected required=[goal], got %v", required)
	}
}

func TestSpawnAgentToolIsReadOnly(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewSpawnAgentTool(m)
	if tool.IsReadOnly() {
		t.Fatal("spawn_agent should not be read-only")
	}
}

func TestSpawnAgentToolIsConcurrencySafe(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewSpawnAgentTool(m)
	if !tool.IsConcurrencySafe(nil) {
		t.Fatal("spawn_agent should be concurrency safe (parallel subagent dispatch)")
	}
}

func TestSpawnAgentToolInterruptBehavior(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewSpawnAgentTool(m)
	if tool.InterruptBehavior() != tools.InterruptCancel {
		t.Fatal("spawn_agent should have InterruptCancel behavior")
	}
}

func TestDAGUnmetDependencies(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)

	// No runs registered — all deps are unmet
	unmet := m.unmetDependencies([]string{"taskA", "taskB"})
	if len(unmet) != 2 {
		t.Fatalf("expected 2 unmet deps, got %d", len(unmet))
	}

	// Register a completed run
	m.runs["taskA"] = &SubagentRun{ID: "sub-1", TaskID: "taskA", Status: "completed"}
	unmet = m.unmetDependencies([]string{"taskA", "taskB"})
	if len(unmet) != 1 || unmet[0] != "taskB" {
		t.Fatalf("expected [taskB] unmet, got %v", unmet)
	}

	// Register second as running (not completed yet)
	m.runs["taskB"] = &SubagentRun{ID: "sub-2", TaskID: "taskB", Status: "running"}
	unmet = m.unmetDependencies([]string{"taskA", "taskB"})
	if len(unmet) != 1 || unmet[0] != "taskB" {
		t.Fatalf("expected [taskB] still unmet (running), got %v", unmet)
	}

	// Complete taskB
	m.runs["taskB"].Status = "completed"
	unmet = m.unmetDependencies([]string{"taskA", "taskB"})
	if len(unmet) != 0 {
		t.Fatalf("expected 0 unmet deps, got %v", unmet)
	}
}

func TestDAGElementNaming(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)

	// First 6 agents get element names
	expected := []struct{ name, element string }{
		{"地 chi", "earth"},
		{"火 hi", "fire"},
		{"水 mizu", "water"},
		{"光 hikari", "light"},
		{"闇 yami", "dark"},
		{"風 kaze", "wind"},
	}

	for i, e := range expected {
		m.counter++
		idx := m.counter - 1
		var name, element string
		if idx < len(elementNames) {
			el := elementNames[idx]
			name = el.Kanji + " " + el.Romaji
			element = el.Element
		}
		if name != e.name {
			t.Fatalf("agent %d: expected name %q, got %q", i, e.name, name)
		}
		if element != e.element {
			t.Fatalf("agent %d: expected element %q, got %q", i, e.element, element)
		}
	}

	// 7th agent falls back to numbered
	m.counter++
	if m.counter != 7 {
		t.Fatalf("expected counter 7, got %d", m.counter)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Fatal("should not truncate short strings")
	}
	result := truncate("this is a very long string that needs truncation", 20)
	if len(result) != 20 {
		t.Fatalf("expected len 20, got %d", len(result))
	}
	if result[len(result)-3:] != "..." {
		t.Fatal("should end with ...")
	}
}
