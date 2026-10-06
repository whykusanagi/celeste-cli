package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// depthGraph builds alphaTop -> bravoMid -> charlieTarget -> deltaLeaf ->
// echoLeaf, with a second path alphaTop -> xrayAlt -> charlieTarget and a
// cycle deltaLeaf -> charlieTarget.
func depthGraph(t *testing.T) *CodeGraphTool {
	t.Helper()
	dir := t.TempDir()
	store, err := codegraph.NewStore(filepath.Join(dir, "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ids := map[string]int64{}
	for i, name := range []string{"alphaTop", "bravoMid", "xrayAlt", "charlieTarget", "deltaLeaf", "echoLeaf"} {
		id, err := store.UpsertSymbol(codegraph.Symbol{
			Name: name, Kind: codegraph.SymbolFunction, Package: "p", File: "p/p.go", Line: 10 * (i + 1),
		})
		require.NoError(t, err)
		ids[name] = id
	}
	for _, e := range [][2]string{
		{"alphaTop", "bravoMid"}, {"alphaTop", "xrayAlt"},
		{"bravoMid", "charlieTarget"}, {"xrayAlt", "charlieTarget"},
		{"charlieTarget", "deltaLeaf"}, {"deltaLeaf", "echoLeaf"},
		{"deltaLeaf", "charlieTarget"},
	} {
		require.NoError(t, store.AddEdge(ids[e[0]], ids[e[1]], codegraph.EdgeCalls))
	}
	return NewCodeGraphTool(codegraph.NewIndexerWithStore(store, dir))
}

func graphQuery(t *testing.T, tool *CodeGraphTool, args map[string]any) string {
	t.Helper()
	res, err := tool.Execute(context.Background(), args, nil)
	require.NoError(t, err)
	require.False(t, res.Error, res.Content)
	return res.Content
}

// #399: depth walks callers-of-callers and callees-of-callees, each symbol
// once, with the hop and the symbol it was reached through.
func TestCodeGraphTool_DepthTraversal(t *testing.T) {
	tool := depthGraph(t)

	d1 := graphQuery(t, tool, map[string]any{"symbol": "charlieTarget", "direction": "callers", "depth": 1})
	assert.Contains(t, d1, "    <- bravoMid (calls) p/p.go:20\n")
	assert.Contains(t, d1, "    <- xrayAlt (calls) p/p.go:30\n")
	assert.Contains(t, d1, "    <- deltaLeaf (calls) p/p.go:50\n")
	assert.NotContains(t, d1, "alphaTop")
	assert.NotContains(t, d1, "hop")

	d2 := graphQuery(t, tool, map[string]any{"symbol": "charlieTarget", "direction": "callers", "depth": 2})
	assert.True(t, strings.HasPrefix(d2, strings.TrimSuffix(d1, "\n")), "depth 2 extends the depth-1 listing:\n%s", d2)
	assert.Equal(t, 1, strings.Count(d2, "alphaTop"), "a symbol reached twice is listed once:\n%s", d2)
	assert.Contains(t, d2, "    <- alphaTop (calls) p/p.go:10 [hop 2, via bravoMid]\n")
	assert.NotContains(t, d2, "<- charlieTarget", "the queried symbol is not its own caller at hop 2")

	d3 := graphQuery(t, tool, map[string]any{"symbol": "alphaTop", "direction": "callees", "depth": 3})
	assert.Contains(t, d3, "    -> bravoMid (calls) p/p.go:20\n")
	assert.Contains(t, d3, "    -> xrayAlt (calls) p/p.go:30\n")
	assert.Contains(t, d3, "    -> charlieTarget (calls) p/p.go:40 [hop 2, via bravoMid]\n")
	assert.Contains(t, d3, "    -> deltaLeaf (calls) p/p.go:50 [hop 3, via charlieTarget]\n")
	assert.Equal(t, 1, strings.Count(d3, "-> charlieTarget"))
	assert.NotContains(t, d3, "echoLeaf", "hop 4 is past depth 3")

	// depth is capped at 3.
	d9 := graphQuery(t, tool, map[string]any{"symbol": "alphaTop", "direction": "callees", "depth": 9})
	assert.Equal(t, d3, d9)
	// Default depth is 1.
	assert.Equal(t, d1, graphQuery(t, tool, map[string]any{"symbol": "charlieTarget", "direction": "callers"}))
}

// An unknown direction is an error, not an empty listing.
func TestCodeGraphTool_UnknownDirection(t *testing.T) {
	tool := depthGraph(t)
	res, err := tool.Execute(context.Background(), map[string]any{"symbol": "charlieTarget", "direction": "sideways"}, nil)
	require.NoError(t, err)
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "callers, callees, both")
}

// hubGraph builds hubTarget with n direct callers (callerNNN), each called
// by its own grandNNN.
func hubGraph(t *testing.T, n int) *CodeGraphTool {
	t.Helper()
	dir := t.TempDir()
	store, err := codegraph.NewStore(filepath.Join(dir, "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	hub, err := store.UpsertSymbol(codegraph.Symbol{Name: "hubTarget", Kind: codegraph.SymbolFunction, Package: "p", File: "p/p.go", Line: 1})
	require.NoError(t, err)
	for i := range n {
		c, err := store.UpsertSymbol(codegraph.Symbol{Name: fmt.Sprintf("caller%03d", i), Kind: codegraph.SymbolFunction, Package: "p", File: "p/c.go", Line: i + 1})
		require.NoError(t, err)
		g, err := store.UpsertSymbol(codegraph.Symbol{Name: fmt.Sprintf("grand%03d", i), Kind: codegraph.SymbolFunction, Package: "p", File: "p/g.go", Line: i + 1})
		require.NoError(t, err)
		require.NoError(t, store.AddEdge(c, hub, codegraph.EdgeCalls))
		require.NoError(t, store.AddEdge(g, c, codegraph.EdgeCalls))
	}
	return NewCodeGraphTool(codegraph.NewIndexerWithStore(store, dir))
}

// The entry cap never cuts the first hop: a default (depth 1) query lists
// every caller, as it did before depth existed. Later hops stop after
// maxGraphHops entries and say so.
func TestCodeGraphTool_HubFirstHopUncapped(t *testing.T) {
	const n = maxGraphHops + 50
	tool := hubGraph(t, n)

	d1 := graphQuery(t, tool, map[string]any{"symbol": "hubTarget", "direction": "callers"})
	assert.Equal(t, n, strings.Count(d1, "    <- caller"), "every first-hop caller is listed")
	assert.NotContains(t, d1, "stopped")

	d2 := graphQuery(t, tool, map[string]any{"symbol": "hubTarget", "direction": "callers", "depth": 2})
	assert.Equal(t, n, strings.Count(d2, "    <- caller"), "every first-hop caller is listed at depth 2")
	assert.Equal(t, maxGraphHops, strings.Count(d2, "    <- grand"), "later hops stop at the cap")
	assert.Contains(t, d2, fmt.Sprintf("... (stopped after %d entries past the first hop; lower depth to see fewer)", maxGraphHops))
}

// Direction is matched case-insensitively, like code_review kinds.
func TestCodeGraphTool_DirectionCaseInsensitive(t *testing.T) {
	tool := depthGraph(t)
	want := graphQuery(t, tool, map[string]any{"symbol": "charlieTarget", "direction": "callers"})
	assert.Equal(t, want, graphQuery(t, tool, map[string]any{"symbol": "charlieTarget", "direction": " Callers "}))
	assert.Equal(t,
		graphQuery(t, tool, map[string]any{"symbol": "charlieTarget", "direction": "both"}),
		graphQuery(t, tool, map[string]any{"symbol": "charlieTarget", "direction": "BOTH"}))
}
