package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// hiddenStub is a builtin-package Tool used to populate the registry.
type hiddenStub struct {
	name string
	desc string
}

func (h hiddenStub) Name() string                               { return h.name }
func (h hiddenStub) Description() string                        { return h.desc }
func (h hiddenStub) Parameters() json.RawMessage                { return nil }
func (h hiddenStub) IsConcurrencySafe(map[string]any) bool      { return true }
func (h hiddenStub) IsReadOnly() bool                           { return true }
func (h hiddenStub) ValidateInput(map[string]any) error         { return nil }
func (h hiddenStub) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (h hiddenStub) Execute(context.Context, map[string]any, chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func TestFindTools_ActivatesAndReports(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(hiddenStub{name: "convert_currency", desc: "Convert between fiat currencies at live rates"})
	r.Register(hiddenStub{name: "get_weather", desc: "Look up the weather"})
	r.SetHidden("convert_currency", true)
	r.SetDiscoveryMode(true)

	// Before: hidden from the mode list.
	before := r.GetTools(tools.ModeChat)
	assert.NotContains(t, toolNames(before), "convert_currency")

	ft := NewFindToolsTool(r)
	res, err := ft.Execute(context.Background(), map[string]any{"query": "exchange money currency"}, nil)
	require.NoError(t, err)
	assert.False(t, res.Error)
	assert.Contains(t, res.Content, "convert_currency")

	// After: activated, now visible.
	after := r.GetTools(tools.ModeChat)
	assert.Contains(t, toolNames(after), "convert_currency")
}

func TestFindTools_NoMatch(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(hiddenStub{name: "get_weather", desc: "weather lookup"})
	ft := NewFindToolsTool(r)
	res, err := ft.Execute(context.Background(), map[string]any{"query": "zzz nonexistent capability"}, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "No matching")
}

func toolNames(ts []tools.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name()
	}
	return out
}
