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
						"description": "Number of hops to traverse. Default: 1, max: 3. Entries past the first hop show their hop and the symbol they were reached through."
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
	switch direction {
	case "callers", "callees", "both":
	default:
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("unknown direction %q; valid directions: callers, callees, both", direction)}, nil
	}
	depth = min(max(depth, 1), maxGraphDepth)

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
		fmt.Fprintf(&b, "## %s (%s) — %s:%d\n", codegraph.DisplayName(sym), sym.Kind, sym.File, sym.Line)
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
			writeGraphHops(&b, "Called by", "<-", walkGraph(store, sym, depth, true))
		}

		if direction == "callees" || direction == "both" {
			writeGraphHops(&b, "Calls", "->", walkGraph(store, sym, depth, false))
		}
		b.WriteString("\n")
	}

	if b.Len() == 0 {
		return tools.ToolResult{Content: "No relationships found."}, nil
	}

	return tools.ToolResult{Content: b.String()}, nil
}

// maxGraphDepth is the most hops code_graph walks from the queried symbol.
const maxGraphDepth = 3

// maxGraphHops caps the entries one direction lists, so depth 3 from a hub
// symbol stays a readable answer.
const maxGraphHops = 200

// graphHop is one related symbol in a code_graph walk: the edge's other end,
// the edge kind, how many hops from the queried symbol it is, and the
// symbol it was reached through (beyond the first hop).
type graphHop struct {
	sym  codegraph.Symbol
	kind codegraph.EdgeKind
	hop  int
	via  string
}

// graphWalk is the result of walkGraph; truncated is set when maxGraphHops
// cut it short.
type graphWalk struct {
	hops      []graphHop
	truncated bool
}

// walkGraph lists the symbols up to depth hops from root, callers (incoming
// edges) or callees (outgoing), breadth first (#399). The first hop lists
// every edge of root, as a one-hop query always has. Later hops list each
// symbol once, at the hop it is first reached, and never root itself.
func walkGraph(store *codegraph.Store, root codegraph.Symbol, depth int, callers bool) graphWalk {
	var w graphWalk
	seen := map[int64]bool{root.ID: true}
	frontier := []codegraph.Symbol{root}
	for hop := 1; hop <= depth && len(frontier) > 0; hop++ {
		var next []codegraph.Symbol
		listed := map[int64]bool{}
		for _, from := range frontier {
			var edges []codegraph.Edge
			var err error
			if callers {
				edges, err = store.GetEdgesTo(from.ID)
			} else {
				edges, err = store.GetEdgesFrom(from.ID)
			}
			if err != nil {
				continue
			}
			for _, e := range edges {
				otherID := e.TargetID
				if callers {
					otherID = e.SourceID
				}
				if hop > 1 && (seen[otherID] || listed[otherID]) {
					continue
				}
				other, err := store.GetSymbol(otherID)
				if err != nil {
					continue
				}
				if len(w.hops) == maxGraphHops {
					w.truncated = true
					return w
				}
				h := graphHop{sym: *other, kind: e.Kind, hop: hop}
				if hop > 1 {
					h.via = codegraph.DisplayName(from)
				}
				w.hops = append(w.hops, h)
				listed[otherID] = true
				if !seen[otherID] {
					seen[otherID] = true
					next = append(next, *other)
				}
			}
		}
		frontier = next
	}
	return w
}

// writeGraphHops renders one direction of a walk under heading. First-hop
// lines keep the one-hop format; later ones add the hop and the symbol they
// were reached through.
func writeGraphHops(b *strings.Builder, heading, arrow string, w graphWalk) {
	if len(w.hops) == 0 {
		return
	}
	fmt.Fprintf(b, "\n  %s:\n", heading)
	for _, h := range w.hops {
		fmt.Fprintf(b, "    %s %s (%s) %s:%d", arrow, codegraph.DisplayName(h.sym), h.kind, h.sym.File, h.sym.Line)
		if h.hop > 1 {
			fmt.Fprintf(b, " [hop %d, via %s]", h.hop, h.via)
		}
		b.WriteString("\n")
	}
	if w.truncated {
		fmt.Fprintf(b, "    ... (stopped at %d entries; lower depth to see fewer)\n", maxGraphHops)
	}
}
