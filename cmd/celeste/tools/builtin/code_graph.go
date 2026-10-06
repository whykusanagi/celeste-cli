package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// CodeGraphTool queries call relationships in the code graph.
// Supports queries like "what calls X", "callers of Z". Edge kinds are
// calls, references (Go: a function taken as a value) and implements (Go:
// interface method to implementing method); embeds are not tracked.
type CodeGraphTool struct {
	BaseTool
	indexer *codegraph.Indexer
}

// NewCodeGraphTool creates a CodeGraphTool backed by the given indexer.
func NewCodeGraphTool(indexer *codegraph.Indexer) *CodeGraphTool {
	return &CodeGraphTool{
		BaseTool: BaseTool{
			ToolName: "code_graph",
			ToolDescription: "Query call relationships in the codebase. " +
				"Find what calls a function and what it calls. Edges are calls; for Go also references " +
				"(a function taken as a value) and implements (interface method -> implementing method). " +
				"An exact name returns every symbol with that name; a qualified name as this tool prints it " +
				"((tui.AppModel).update, commands.Execute) picks one. " +
				"First use code_search to find the symbol name, then use code_graph to explore relationships.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"symbol": {
						"type": "string",
						"description": "Symbol name or qualified name to query relationships for, e.g. update, (tui.AppModel).update, (*T).M, pkg.Func."
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

	res, err := t.indexer.LookupSymbol(symbolName)
	if err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("search error: %s", err)}, nil
	}
	syms := res.Symbols
	if len(syms) == 0 {
		return tools.ToolResult{Content: fmt.Sprintf("Symbol '%s' not found in the code graph.", symbolName)}, nil
	}

	var b strings.Builder
	switch {
	case res.Match == codegraph.MatchPartial && len(syms) > 1:
		fmt.Fprintf(&b, "No symbol is named '%s'. %d symbols match it in part; query one by its qualified name:\n\n", symbolName, len(syms))
		writeCandidates(&b, syms, maxGraphCandidates)
		return tools.ToolResult{Content: b.String()}, nil
	case len(syms) > maxGraphDetailed:
		fmt.Fprintf(&b, "%d symbols are named '%s'. Query one by its qualified name:\n\n", len(syms), symbolName)
		writeCandidates(&b, syms, len(syms))
		return tools.ToolResult{Content: b.String()}, nil
	case res.Match == codegraph.MatchPartial:
		fmt.Fprintf(&b, "No symbol is named '%s'; the one partial match:\n\n", symbolName)
	case len(syms) > 1:
		fmt.Fprintf(&b, "%d symbols are named '%s':\n\n", len(syms), symbolName)
	}

	store := t.indexer.Store()
	names := codegraph.QualifiedNames(syms)
	for i, sym := range syms {
		name := codegraph.DisplayName(sym)
		if len(syms) > 1 {
			name = names[i]
		}
		fmt.Fprintf(&b, "## %s (%s) — %s:%d\n", name, sym.Kind, sym.File, sym.Line)
		if sym.Signature != "" {
			fmt.Fprintf(&b, "  %s\n", sym.Signature)
		}
		if sym.Implements != "" {
			fmt.Fprintf(&b, "  Implements: %s\n", strings.ReplaceAll(sym.Implements, ",", ", "))
		}
		if store.FileResolution(sym.File) == codegraph.GoResolutionApproximate {
			b.WriteString("  (approximate: this file did not type-check; its call edges may be incomplete or resolved by name)\n")
		}

		// Get edges
		if direction == "callers" || direction == "both" {
			edges, err := store.GetEdgesTo(sym.ID)
			if err == nil && len(edges) > 0 {
				fmt.Fprintf(&b, "\n  Called by:\n")
				for _, e := range edges {
					if caller, err := store.GetSymbol(e.SourceID); err == nil {
						fmt.Fprintf(&b, "    <- %s (%s) %s:%d\n", codegraph.DisplayName(*caller), e.Kind, caller.File, caller.Line)
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
						fmt.Fprintf(&b, "    -> %s (%s) %s:%d\n", codegraph.DisplayName(*callee), e.Kind, callee.File, callee.Line)
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

const (
	// maxGraphDetailed is the most same-named symbols one answer details
	// with their edges; more are listed for the caller to pick from.
	maxGraphDetailed = 8
	// maxGraphCandidates caps the list of partial matches.
	maxGraphCandidates = 50
)

// writeCandidates lists symbols by qualified name, kind and file:line, at
// most limit of them.
func writeCandidates(b *strings.Builder, syms []codegraph.Symbol, limit int) {
	names := codegraph.QualifiedNames(syms)
	for i, sym := range syms {
		if i == limit {
			fmt.Fprintf(b, "  … and %d more\n", len(syms)-limit)
			break
		}
		fmt.Fprintf(b, "  %s (%s) — %s:%d\n", names[i], sym.Kind, sym.File, sym.Line)
	}
}
