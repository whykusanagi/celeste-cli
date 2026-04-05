package builtin

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
)

func TestCodeReviewTool_Execute(t *testing.T) {
	// Set up an in-memory code graph store with test data.
	dir := t.TempDir()
	store, err := codegraph.NewStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	// Create symbols: a stub (no outgoing edges) and a non-stub (has outgoing edges).
	stubID, err := store.UpsertSymbol(codegraph.Symbol{
		Name: "ProcessOrder", Kind: codegraph.SymbolFunction,
		Package: "orders", File: "orders/process.go", Line: 15,
	})
	require.NoError(t, err)

	callerID, err := store.UpsertSymbol(codegraph.Symbol{
		Name: "HandleRequest", Kind: codegraph.SymbolFunction,
		Package: "server", File: "server/handler.go", Line: 30,
	})
	require.NoError(t, err)

	calleeID, err := store.UpsertSymbol(codegraph.Symbol{
		Name: "Respond", Kind: codegraph.SymbolFunction,
		Package: "server", File: "server/handler.go", Line: 50,
	})
	require.NoError(t, err)

	// HandleRequest calls Respond — so HandleRequest is NOT a stub.
	err = store.AddEdge(callerID, calleeID, codegraph.EdgeCalls)
	require.NoError(t, err)

	// HandleRequest calls ProcessOrder — so ProcessOrder has an incoming edge.
	err = store.AddEdge(callerID, stubID, codegraph.EdgeCalls)
	require.NoError(t, err)

	// Create an indexer wrapping this store.
	indexer := codegraph.NewIndexerWithStore(store, dir)
	tool := NewCodeReviewTool(indexer)

	assert.Equal(t, "code_review", tool.Name())
	assert.True(t, tool.IsReadOnly())

	// Execute with STUB kind only — should find ProcessOrder and Respond as stubs.
	result, err := tool.Execute(context.Background(), map[string]any{
		"kinds": "STUB",
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)
	assert.Contains(t, result.Content, "ProcessOrder")
	assert.Contains(t, result.Content, "Respond")
	assert.NotContains(t, result.Content, "HandleRequest")
}

func TestCodeReviewTool_FilterLeaf(t *testing.T) {
	dir := t.TempDir()
	store, err := codegraph.NewStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	// Create a leaf function that should be filtered out.
	_, err = store.UpsertSymbol(codegraph.Symbol{
		Name: "String", Kind: codegraph.SymbolMethod,
		Package: "types", File: "types/model.go", Line: 10,
	})
	require.NoError(t, err)

	// Create a constructor that should be filtered out.
	_, err = store.UpsertSymbol(codegraph.Symbol{
		Name: "NewService", Kind: codegraph.SymbolFunction,
		Package: "svc", File: "svc/service.go", Line: 5,
	})
	require.NoError(t, err)

	indexer := codegraph.NewIndexerWithStore(store, dir)
	tool := NewCodeReviewTool(indexer)

	result, err := tool.Execute(context.Background(), map[string]any{
		"kinds": "STUB",
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No issues detected")
}

func TestCodeReviewTool_ExcludeTests(t *testing.T) {
	dir := t.TempDir()
	store, err := codegraph.NewStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	// A stub in a test file.
	_, err = store.UpsertSymbol(codegraph.Symbol{
		Name: "helperSetup", Kind: codegraph.SymbolFunction,
		Package: "pkg", File: "pkg/handler_test.go", Line: 20,
	})
	require.NoError(t, err)

	// A stub in a non-test file.
	_, err = store.UpsertSymbol(codegraph.Symbol{
		Name: "Placeholder", Kind: codegraph.SymbolFunction,
		Package: "pkg", File: "pkg/handler.go", Line: 10,
	})
	require.NoError(t, err)

	indexer := codegraph.NewIndexerWithStore(store, dir)
	tool := NewCodeReviewTool(indexer)

	// Default: exclude tests.
	result, err := tool.Execute(context.Background(), map[string]any{
		"kinds": "STUB",
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "Placeholder")
	assert.NotContains(t, result.Content, "helperSetup")

	// Include tests.
	result, err = tool.Execute(context.Background(), map[string]any{
		"kinds":         "STUB",
		"include_tests": true,
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "Placeholder")
	assert.Contains(t, result.Content, "helperSetup")
}

func TestCodeReviewTool_AllCategories(t *testing.T) {
	dir := t.TempDir()
	store, err := codegraph.NewStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	// A stub function — should appear in STUB category
	_, err = store.UpsertSymbol(codegraph.Symbol{
		Name: "ProcessOrder", Kind: codegraph.SymbolFunction,
		Package: "orders", File: "orders/process.go", Line: 15,
	})
	require.NoError(t, err)

	indexer := codegraph.NewIndexerWithStore(store, dir)
	tool := NewCodeReviewTool(indexer)

	// ALL categories (default)
	result, err := tool.Execute(context.Background(), map[string]any{}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)
	// Should find the stub
	assert.Contains(t, result.Content, "ProcessOrder")
	assert.Contains(t, result.Content, "STUB")
}
