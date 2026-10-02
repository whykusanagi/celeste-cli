package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type namedTool struct{ name string }

func (n namedTool) Name() string                          { return n.name }
func (n namedTool) Description() string                   { return "" }
func (n namedTool) Parameters() json.RawMessage           { return json.RawMessage(`{"type":"object"}`) }
func (n namedTool) IsConcurrencySafe(map[string]any) bool { return true }
func (n namedTool) IsReadOnly() bool                      { return true }
func (n namedTool) ValidateInput(map[string]any) error    { return nil }
func (n namedTool) InterruptBehavior() InterruptBehavior  { return InterruptCancel }
func (n namedTool) Execute(context.Context, map[string]any, chan<- ProgressEvent) (ToolResult, error) {
	return ToolResult{}, nil
}

// sliceTool is not comparable: comparing two of them with == would panic.
type sliceTool struct {
	namedTool
	tags []string
}

func TestRegistryAddRefusesATakenName(t *testing.T) {
	r := NewRegistry()
	builtin := &namedTool{"read_file"}
	r.Register(builtin)
	err := r.Add(&namedTool{"read_file"})
	if !errors.Is(err, ErrToolNameTaken) || !strings.Contains(err.Error(), "read_file") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := r.Get("read_file"); got != Tool(builtin) {
		t.Fatalf("the builtin was replaced by %v", got)
	}
	own := &namedTool{"mcp__x__y"}
	if err := r.Add(own); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(own); err != nil {
		t.Fatalf("re-adding the same tool (a reconnect) must succeed: %v", err)
	}
}

func TestRegistryAddNonComparableToolsDoNotPanic(t *testing.T) {
	r := NewRegistry()
	r.Register(sliceTool{namedTool{"x"}, nil})
	if err := r.Add(sliceTool{namedTool{"x"}, nil}); !errors.Is(err, ErrToolNameTaken) {
		t.Fatalf("err = %v", err)
	}
	r.Register(sliceTool{namedTool{"x"}, nil})
	if got := r.Overwritten(); len(got) != 1 || got[0] != "x" {
		t.Fatalf("Overwritten = %v", got)
	}
}

func TestRegistryAddKeepsModes(t *testing.T) {
	r := NewRegistry()
	if err := r.Add(&namedTool{"agent_only"}, ModeAgent); err != nil {
		t.Fatal(err)
	}
	for _, tl := range r.GetTools(ModeChat) {
		if tl.Name() == "agent_only" {
			t.Fatal("a tool added for agent mode only is offered in chat")
		}
	}
}

func TestRegistryOverwrittenListsReplacedNames(t *testing.T) {
	r := NewRegistry()
	a := &namedTool{"a"}
	r.Register(a)
	r.Register(a) // the same tool again is not an overwrite
	if got := r.Overwritten(); len(got) != 0 {
		t.Fatalf("Overwritten = %v, want none", got)
	}
	r.RegisterWithModes(&namedTool{"a"}, ModeChat)
	if got := r.Overwritten(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("Overwritten = %v, want [a]", got)
	}
}

// A JSON custom tool (a 1.x "skill", W4a-2's commands) can no longer take a
// builtin's name; the rest of the directory still loads.
func TestCustomToolsNeverReplaceATool(t *testing.T) {
	dir := t.TempDir()
	write := func(file, name string) {
		body := `{"name":"` + name + `","description":"d","parameters":{"type":"object"},"command":"true"}`
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.json", "read_file")
	write("b.json", "my_tool")
	r := NewRegistry()
	builtin := &namedTool{"read_file"}
	r.Register(builtin)
	err := r.LoadCustomTools(dir)
	if !errors.Is(err, ErrToolNameTaken) || !strings.Contains(err.Error(), "read_file") {
		t.Fatalf("err = %v, want a name-taken error naming read_file", err)
	}
	if got, _ := r.Get("read_file"); got != Tool(builtin) {
		t.Fatalf("read_file was replaced by %T", got)
	}
	if _, ok := r.Get("my_tool"); !ok {
		t.Fatal("the other custom tool must still load")
	}
}
