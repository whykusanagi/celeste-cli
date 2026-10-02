package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type retainTool struct{ name string }

func (r retainTool) Name() string                          { return r.name }
func (r retainTool) Description() string                   { return "" }
func (r retainTool) Parameters() json.RawMessage           { return json.RawMessage(`{"type":"object"}`) }
func (r retainTool) IsConcurrencySafe(map[string]any) bool { return true }
func (r retainTool) IsReadOnly() bool                      { return true }
func (r retainTool) ValidateInput(map[string]any) error    { return nil }
func (r retainTool) InterruptBehavior() InterruptBehavior  { return InterruptCancel }
func (r retainTool) Execute(context.Context, map[string]any, chan<- ProgressEvent) (ToolResult, error) {
	return ToolResult{}, nil
}

func TestRetainKeepsOnlyMatches(t *testing.T) {
	r := NewRegistry()
	r.Register(retainTool{"keep_a"})
	r.RegisterWithModes(retainTool{"keep_b"}, ModeAgent)
	r.Register(retainTool{"drop_a"})
	r.RegisterWithModes(retainTool{"drop_b"}, ModeChat)
	r.SetDiscoveryMode(true)
	r.SetHidden("drop_b", true)
	r.Activate("drop_b")
	n := r.Retain(func(t Tool) bool { return t.Name()[:4] == "keep" })
	if n != 2 {
		t.Fatalf("removed %d, want 2", n)
	}
	if _, ok := r.Get("drop_a"); ok {
		t.Fatal("drop_a kept")
	}
	if got := r.GetTools(ModeAgent); len(got) != 2 {
		t.Fatalf("agent tools = %d, want 2", len(got))
	}
	// A tool registered again under a dropped name starts clean: not hidden.
	r.Register(retainTool{"drop_b"})
	if _, ok := r.Get("drop_b"); !ok || len(r.GetTools(ModeAgent)) != 3 {
		t.Fatal("re-registered tool should be visible")
	}
	if r.Retain(nil) != 0 {
		t.Fatal("a nil filter keeps everything")
	}
}
