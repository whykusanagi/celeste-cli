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
	direction := strings.ToLower(strings.TrimSpace(getStringArg(input, "direction", "both")))
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

		// Get edges. A failed query is the tool's error: a graph missing
		// the branches it could not read is not a complete answer.
		if direction == "callers" || direction == "both" {
			w, err := walkGraph(store, sym, depth, true)
			if err != nil {
				return tools.ToolResult{Error: true, Content: fmt.Sprintf("graph error for %s: %s", name, err)}, nil
			}
			writeGraphHops(&b, "Called by", "<-", w)
		}

		if direction == "callees" || direction == "both" {
			w, err := walkGraph(store, sym, depth, false)
			if err != nil {
				return tools.ToolResult{Error: true, Content: fmt.Sprintf("graph error for %s: %s", name, err)}, nil
			}
			writeGraphHops(&b, "Calls", "->", w)
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

// maxGraphDepth is the most hops code_graph walks from the queried symbol.
const maxGraphDepth = 3

// maxGraphHops caps the entries one direction lists past the first hop, so
// depth 3 from a hub symbol stays a readable answer. The first hop is never
// capped: a one-hop query lists every edge, as it always has.
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
// cut its later hops short.
type graphWalk struct {
	hops      []graphHop
	truncated bool
}

// walkGraph lists the symbols up to depth hops from root, callers (incoming
// edges) or callees (outgoing), breadth first (#399). The first hop lists
// every edge of root, as a one-hop query always has. Later hops list each
// symbol once, at the hop it is first reached, and never root itself. A
// failed edge query or symbol read stops the walk with that error.
func walkGraph(store *codegraph.Store, root codegraph.Symbol, depth int, callers bool) (graphWalk, error) {
	var w graphWalk
	later := 0 // entries past the first hop, which maxGraphHops caps
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
				return w, fmt.Errorf("edges of %s: %w", codegraph.DisplayName(from), err)
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
					return w, fmt.Errorf("symbol %d (an edge of %s): %w", otherID, codegraph.DisplayName(from), err)
				}
				if hop > 1 {
					if later == maxGraphHops {
						w.truncated = true
						return w, nil
					}
					later++
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
	return w, nil
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
		fmt.Fprintf(b, "    ... (stopped after %d entries past the first hop; lower depth to see fewer)\n", maxGraphHops)
	}
}
