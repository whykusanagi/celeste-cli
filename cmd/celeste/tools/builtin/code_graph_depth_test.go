package builtin

import (
	"context"
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
