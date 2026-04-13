package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// CodeSearchTool searches the code graph for symbols matching a query.
// Supports semantic search (MinHash similarity) and keyword search (exact match).
type CodeSearchTool struct {
	BaseTool
	indexer *codegraph.Indexer
}

// NewCodeSearchTool creates a CodeSearchTool backed by the given indexer.
func NewCodeSearchTool(indexer *codegraph.Indexer) *CodeSearchTool {
	return &CodeSearchTool{
		BaseTool: BaseTool{
			ToolName: "code_search",
			ToolDescription: "Search the codebase for symbols (functions, types, interfaces) by concept or keyword. " +
				"Use mode 'semantic' to find code related to a concept (e.g., 'authentication session validation'). " +
				"Use mode 'keyword' for exact name matching (e.g., 'HandleRequest'). " +
				"Semantic results include per-result confidence metadata: a match % (MinHash Jaccard " +
				"estimate), an edge count (how many incoming/outgoing call-graph edges the symbol has), " +
				"and a list of warnings like 'demoted: mock path', 'zero edges', 'low confidence', or " +
				"'demoted: declaration-only file'. Use these warnings to decide which results to trust — " +
				"a clean-path result with edges > 0 and no warnings is a strong match; a result with " +
				"'demoted' warnings is test/mock code that was pushed below production matches; a result " +
				"with 'zero edges' on a non-Go language may be a parser limitation (see SPEC §8.2 Issue #2), " +
				"not actual dead code.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "Search query — concept description for semantic mode, symbol name for keyword mode."
					},
					"mode": {
						"type": "string",
						"enum": ["semantic", "keyword"],
						"description": "Search mode. Default: semantic."
					},
					"limit": {
						"type": "number",
						"description": "Maximum results to return. Default: 10."
					}
				},
				"required": ["query"]
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
			RequiredFields:  []string{"query"},
		},
		indexer: indexer,
	}
}

func (t *CodeSearchTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	query := getStringArg(input, "query", "")
	mode := getStringArg(input, "mode", "semantic")
	limit := getIntArg(input, "limit", 10)

	if query == "" {
		return tools.ToolResult{Error: true, Content: "query is required"}, nil
	}

	var resultText string

	switch mode {
	case "semantic":
		results, err := t.indexer.SemanticSearch(query, limit)
		if err != nil {
			return tools.ToolResult{Error: true, Content: fmt.Sprintf("semantic search error: %s", err)}, nil
		}
		if len(results) == 0 {
			resultText = "No symbols found matching the query."
		} else {
			var b strings.Builder
			fmt.Fprintf(&b, "Found %d symbols matching '%s':\n\n", len(results), query)
			fmt.Fprintln(&b, "Each result shows similarity %, edge count, and any confidence warnings.")
			fmt.Fprintln(&b, "Warnings like 'demoted: mock path', 'zero edges', and 'low confidence' tell")
			fmt.Fprintln(&b, "you WHY to be skeptical — a clean-path result with edges is your strongest match.")
			fmt.Fprintln(&b)
			for i, r := range results {
				fmt.Fprintf(&b, "%d. %s (%s) — %s:%d [%.0f%% match, edges=%d]\n",
					i+1, r.Symbol.Name, r.Symbol.Kind,
					r.Symbol.File, r.Symbol.Line,
					r.Similarity*100, r.EdgeCount)
				if r.Symbol.Signature != "" {
					fmt.Fprintf(&b, "   %s\n", r.Symbol.Signature)
				}
				if len(r.ConfidenceWarnings) > 0 {
					fmt.Fprintf(&b, "   ⚠ %s\n", strings.Join(r.ConfidenceWarnings, "; "))
				}
			}
			resultText = b.String()
		}

	case "keyword":
		syms, err := t.indexer.KeywordSearch(query, limit)
		if err != nil {
			return tools.ToolResult{Error: true, Content: fmt.Sprintf("keyword search error: %s", err)}, nil
		}
		if len(syms) == 0 {
			resultText = fmt.Sprintf("No symbols found matching '%s'.", query)
		} else {
			var b strings.Builder
			fmt.Fprintf(&b, "Found %d symbols matching '%s':\n\n", len(syms), query)
			for i, s := range syms {
				fmt.Fprintf(&b, "%d. %s (%s) — %s:%d\n", i+1, s.Name, s.Kind, s.File, s.Line)
				if s.Signature != "" {
					fmt.Fprintf(&b, "   %s\n", s.Signature)
				}
			}
			resultText = b.String()
		}

	default:
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("unknown mode: %s (use 'semantic' or 'keyword')", mode)}, nil
	}

	return tools.ToolResult{Content: resultText}, nil
}
