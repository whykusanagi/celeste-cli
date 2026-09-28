package hooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

type echoTool struct{}

func (echoTool) Name() string                               { return "echo" }
func (echoTool) Description() string                        { return "echo" }
func (echoTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (echoTool) IsConcurrencySafe(map[string]any) bool      { return true }
func (echoTool) IsReadOnly() bool                           { return true }
func (echoTool) ValidateInput(map[string]any) error         { return nil }
func (echoTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (echoTool) Execute(_ context.Context, in map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "echo:" + in["text"].(string)}, nil
}

func TestToolHooksThroughRegistry(t *testing.T) {
	r, _ := testRunner(t, v2(t, EventPreToolUse, "rewrite", "text", "rewritten"), v2(t, EventPostToolUse, "context", "checked"))
	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	th := r.ToolHooks()
	require.NotNil(t, th)
	reg.SetHookRunner(th)
	res, err := reg.Execute(context.Background(), "echo", map[string]any{"text": "orig"})
	require.NoError(t, err)
	assert.Contains(t, res.Content, "echo:rewritten")
	assert.Contains(t, res.Content, "checked")
}

func TestToolHooksNilWithoutToolEvents(t *testing.T) {
	r, _ := testRunner(t, v2(t, EventStop, "allow"))
	assert.Nil(t, r.ToolHooks())
}
