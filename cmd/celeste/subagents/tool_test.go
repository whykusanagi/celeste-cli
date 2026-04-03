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
	if tool.IsConcurrencySafe(nil) {
		t.Fatal("spawn_agent should not be concurrency safe")
	}
}

func TestSpawnAgentToolInterruptBehavior(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewSpawnAgentTool(m)
	if tool.InterruptBehavior() != tools.InterruptCancel {
		t.Fatal("spawn_agent should have InterruptCancel behavior")
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
