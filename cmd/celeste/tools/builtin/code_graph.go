package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// CodeGraphTool queries structural relationships in the code graph.
// Supports queries like "what calls X", "what implements Y", "callers of Z".
type CodeGraphTool struct {
	BaseTool
	indexer *codegraph.Indexer
}

// NewCodeGraphTool creates a CodeGraphTool backed by the given indexer.
func NewCodeGraphTool(indexer *codegraph.Indexer) *CodeGraphTool {
	return &CodeGraphTool{
		BaseTool: BaseTool{
			ToolName: "code_graph",
			ToolDescription: "Query structural relationships in the codebase. " +
				"Find what calls a function, what implements an interface, what a symbol references, etc. " +
				"First use code_search to find the symbol name, then use code_graph to explore relationships.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"symbol": {
						"type": "string",
						"description": "Symbol name to query relationships for."
					},
					"direction": {
						"type": "string",
						"enum": ["callers", "callees", "both"],
						"description": "Edge direction: callers (who calls this), callees (what this calls), both. Default: both."
					},
					"depth": {
						"type": "number",
						"description": "Number of hops to traverse. Default: 1, max: 3."
					}
				},
				"required": ["symbol"]
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
			RequiredFields:  []string{"symbol"},
		},
		indexer: indexer,
	}
}

func (t *CodeGraphTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	symbolName := getStringArg(input, "symbol", "")
	direction := getStringArg(input, "direction", "both")
	depth := getIntArg(input, "depth", 1)

	if symbolName == "" {
		return tools.ToolResult{Error: true, Content: "symbol is required"}, nil
	}
	if depth > 3 {
		depth = 3
	}
	// depth is accepted but only 1-hop traversal is implemented currently
	_ = depth

	// Find the symbol by name
	syms, err := t.indexer.KeywordSearch(symbolName, 5)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("search error: %s", err)}, nil
	}

	if len(syms) == 0 {
		return tools.ToolResult{Content: fmt.Sprintf("Symbol '%s' not found in the code graph.", symbolName)}, nil
	}

	var b strings.Builder
	store := t.indexer.Store()

	for _, sym := range syms {
		fmt.Fprintf(&b, "## %s (%s) — %s:%d\n", sym.Name, sym.Kind, sym.File, sym.Line)
		if sym.Signature != "" {
			fmt.Fprintf(&b, "  %s\n", sym.Signature)
		}

		// Get edges
		if direction == "callers" || direction == "both" {
			edges, err := store.GetEdgesTo(sym.ID)
			if err == nil && len(edges) > 0 {
				fmt.Fprintf(&b, "\n  Called by:\n")
				for _, e := range edges {
					if caller, err := store.GetSymbol(e.SourceID); err == nil {
						fmt.Fprintf(&b, "    <- %s (%s) %s:%d\n", caller.Name, e.Kind, caller.File, caller.Line)
					}
				}
			}
		}

		if direction == "callees" || direction == "both" {
			edges, err := store.GetEdgesFrom(sym.ID)
			if err == nil && len(edges) > 0 {
				fmt.Fprintf(&b, "\n  Calls:\n")
				for _, e := range edges {
					if callee, err := store.GetSymbol(e.TargetID); err == nil {
						fmt.Fprintf(&b, "    -> %s (%s) %s:%d\n", callee.Name, e.Kind, callee.File, callee.Line)
					}
				}
			}
		}
		b.WriteString("\n")
	}

	if b.Len() == 0 {
		return tools.ToolResult{Content: "No relationships found."}, nil
	}

	return tools.ToolResult{Content: b.String()}, nil
}
