package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTool is a minimal Tool for registry tests.
type stubTool struct {
	name string
	desc string
}

func (s stubTool) Name() string                          { return s.name }
func (s stubTool) Description() string                   { return s.desc }
func (s stubTool) Parameters() json.RawMessage           { return nil }
func (s stubTool) IsConcurrencySafe(map[string]any) bool { return true }
func (s stubTool) IsReadOnly() bool                      { return true }
func (s stubTool) ValidateInput(map[string]any) error    { return nil }
func (s stubTool) InterruptBehavior() InterruptBehavior  { return InterruptCancel }
func (s stubTool) Execute(context.Context, map[string]any, chan<- ProgressEvent) (ToolResult, error) {
	return ToolResult{}, nil
}

func names(ts []Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name()
	}
	return out
}

func TestRegistry_DiscoveryModeOff_NoHiding(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "visible", desc: "always here"})
	r.Register(stubTool{name: "buried", desc: "extra"})
	r.SetHidden("buried", true) // marked hidden, but discovery is OFF

	got := names(r.GetTools(ModeChat))
	assert.Contains(t, got, "visible")
	assert.Contains(t, got, "buried", "hidden has no effect while discoveryMode is off")
}

func TestRegistry_DiscoveryModeOn_HidesUntilActivated(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "visible", desc: "always here"})
	r.Register(stubTool{name: "buried", desc: "extra"})
	r.SetHidden("buried", true)
	r.SetDiscoveryMode(true)

	got := names(r.GetTools(ModeChat))
	assert.Contains(t, got, "visible")
	assert.NotContains(t, got, "buried", "hidden+not-activated excluded under discoveryMode")

	// GetAll stays unfiltered so find_tools can still see it.
	assert.Contains(t, names(r.GetAll()), "buried")

	r.Activate("buried")
	require.Contains(t, names(r.GetTools(ModeChat)), "buried", "activation restores visibility")
}
