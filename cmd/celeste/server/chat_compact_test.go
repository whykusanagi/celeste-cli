package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// The MCP compactor protects the results added since the last request.
func TestMCPChatCompactorProtectsUnseenResults(t *testing.T) {
	c := &chatCompactor{window: 40_000, meter: compact.NewMeter(2_000), store: &compact.Store{Dir: t.TempDir()}}
	history := []loop.Message{{Role: "user", Content: "read everything"}}
	_, _, _ = c.Compact(context.Background(), history, nil, false) // request 1
	asst := loop.Message{Role: "assistant"}
	var results []loop.Message
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("n%d", i)
		asst.ToolCalls = append(asst.ToolCalls, tui.ToolCallInfo{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"%d"}`, i)})
		results = append(results, loop.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 8_000)})
	}
	history = append(append(history, asst), results...)
	out, _, _ := c.Compact(context.Background(), history, &llm.TokenUsage{PromptTokens: 3_000}, false)
	for _, m := range out {
		if m.Role == "tool" && strings.Contains(m.Content, "recall_tool_result with id") {
			t.Fatalf("elided %s before the model saw it", m.ToolCallID)
		}
	}
}
