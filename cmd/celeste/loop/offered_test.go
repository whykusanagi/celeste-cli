package loop

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// The loop runs only the tools the turn offered the model: a registered
// tool of another runtime mode, or one hidden until find_tools activates
// it, is refused without running.
func TestLoopRunsOnlyOfferedTools(t *testing.T) {
	hermetic(t)
	var ran atomic.Int32
	count := func(context.Context, map[string]any) (tools.ToolResult, error) {
		ran.Add(1)
		return tools.ToolResult{Content: "ran"}, nil
	}
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{
			{ID: "c1", Name: "chat_only", Args: `{}`},
			{ID: "c2", Name: "hidden", Args: `{}`},
		}},
		fakeprovider.Turn{Text: "done"},
	)
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "echo", readOnly: true})
	reg.RegisterWithModes(&fakeTool{name: "chat_only", readOnly: true, run: count}, tools.ModeChat)
	reg.Register(&fakeTool{name: "hidden", readOnly: true, run: count})
	reg.SetDiscoveryMode(true)
	reg.SetHidden("hidden", true)
	client := newClient(srv, reg)
	client.SetToolMode(tools.ModeAgent)
	l := &Loop{Client: client, Tools: reg, Limits: DefaultLimits(), SpillDir: t.TempDir()}

	msgs, _, err := l.Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	if n := ran.Load(); n != 0 {
		t.Fatalf("%d tools that were not offered ran", n)
	}
	refused := 0
	for _, m := range msgs {
		if m.Role == "tool" && strings.Contains(m.Content, "not offered") {
			refused++
		}
	}
	if refused != 2 {
		t.Fatalf("refused = %d, want 2: %+v", refused, msgs)
	}
}
