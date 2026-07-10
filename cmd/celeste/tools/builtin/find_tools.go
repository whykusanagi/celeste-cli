package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// FindToolsTool lets the model search for and activate additional tools by
// capability when the tool it needs is hidden under discovery mode. It searches
// ALL registered tools (via GetAll, which is unfiltered) with BM25.
type FindToolsTool struct {
	BaseTool
	registry *tools.Registry
}

// NewFindToolsTool constructs the find_tools tool. It must be registered
// VISIBLE (never hidden), or the model cannot escape an empty tool list.
func NewFindToolsTool(registry *tools.Registry) *FindToolsTool {
	return &FindToolsTool{
		BaseTool: BaseTool{
			ToolName:        "find_tools",
			ToolDescription: "Search for and activate additional tools by capability when the tool you need is not currently listed. Pass a natural-language query describing what you want to do (e.g. 'convert currency', 'read a PDF'). Matching tools become available for the rest of the session.",
			ToolParameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Natural-language description of the capability you need"},"top_n":{"type":"integer","description":"Max tools to activate (default 5)"}},"required":["query"]}`),
			ReadOnly:        true,
			RequiredFields:  []string{"query"},
		},
		registry: registry,
	}
}

func (t *FindToolsTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	query, _ := input["query"].(string)
	topN := 5
	if v, ok := input["top_n"].(float64); ok && v > 0 { // JSON numbers decode to float64
		topN = int(v)
	}

	ix := tools.BuildToolIndex(t.registry.GetAll())
	matches := ix.Search(query, topN)
	if len(matches) == 0 {
		return tools.ToolResult{Content: "No matching tools found for query: " + query}, nil
	}

	t.registry.Activate(matches...)

	var b strings.Builder
	fmt.Fprintf(&b, "Activated %d tool(s) — now available to call:\n", len(matches))
	for _, name := range matches {
		if tool, ok := t.registry.Get(name); ok {
			fmt.Fprintf(&b, "- %s: %s\n", tool.Name(), tool.Description())
		}
	}
	return tools.ToolResult{Content: b.String()}, nil
}
