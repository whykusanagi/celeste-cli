package loop

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
)

// guard trips when the same non-empty signature is observed limit times in
// a row. An empty signature resets it; a zero limit is off.
type guard struct {
	limit  int
	streak int
	last   string
}

func (g *guard) observe(sig string) bool {
	if g.limit <= 0 {
		return false
	}
	if sig == "" {
		g.streak, g.last = 0, ""
		return false
	}
	if sig == g.last {
		g.streak++
	} else {
		g.streak, g.last = 1, sig
	}
	return g.streak >= g.limit
}

// batchSig includes arguments, so legitimate bulk work (distinct args) never
// trips the identical-call guard; only a true stuck loop does.
func batchSig(calls []llm.ToolCallResult) string {
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, c.Name+"("+c.Arguments+")")
	}
	return strings.Join(parts, ",")
}

// parseTextToolCalls extracts <tool_call>{"name":..,"arguments":{..}}</tool_call>
// blocks, for models and proxies that describe tool calls in text instead of
// issuing native ones. IDs are "text-tc-N".
func parseTextToolCalls(content string) []llm.ToolCallResult {
	var results []llm.ToolCallResult
	remaining := content
	for {
		start := strings.Index(remaining, "<tool_call>")
		if start < 0 {
			break
		}
		after := remaining[start+len("<tool_call>"):]
		end := strings.Index(after, "</tool_call>")
		if end < 0 {
			break
		}
		var call struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(after[:end])), &call); err == nil && call.Name != "" {
			argsJSON, _ := json.Marshal(call.Arguments)
			results = append(results, llm.ToolCallResult{
				ID:        fmt.Sprintf("text-tc-%d", len(results)),
				Name:      call.Name,
				Arguments: string(argsJSON),
			})
		}
		remaining = after[end+len("</tool_call>"):]
	}
	return results
}
