package builtin

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// buildBrokenGraphIndex indexes a two-function module and returns the
// indexer with a second raw handle on its database, which a test uses to
// break the graph under the tool.
func buildBrokenGraphIndex(t *testing.T) (*codegraph.Indexer, *sql.DB) {
	t.Helper()
	ws := t.TempDir()
	files := map[string]string{
		"go.mod":  "module example.com/m\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc helper() {}\n\nfunc main() { helper() }\n",
	}
	for name, body := range files {
		p := filepath.Join(ws, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	dbPath := filepath.Join(t.TempDir(), "cg.db")
	idx, err := codegraph.NewIndexer(ws, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	raw, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(10000)&_pragma=foreign_keys(0)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	return idx, raw
}

// #407: an edge query that fails is a tool error, not a graph with that
// branch silently missing.
func TestCodeGraphTool_EdgeQueryFailureIsError(t *testing.T) {
	idx, raw := buildBrokenGraphIndex(t)
	tool := NewCodeGraphTool(idx)
	ok, err := tool.Execute(context.Background(), map[string]any{"symbol": "helper", "direction": "callers"}, nil)
	require.NoError(t, err)
	require.False(t, ok.Error, ok.Content)
	require.Contains(t, ok.Content, "main")

	_, err = raw.Exec(`DROP TABLE edges`)
	require.NoError(t, err)
	for _, dir := range []string{"callers", "callees", "both"} {
		res, err := tool.Execute(context.Background(), map[string]any{"symbol": "helper", "direction": dir}, nil)
		require.NoError(t, err)
		assert.True(t, res.Error, "direction %s: %s", dir, res.Content)
		assert.Contains(t, res.Content, "edges", dir)
	}
}

// #407: an edge whose other end cannot be read is a tool error too.
func TestCodeGraphTool_GetSymbolFailureIsError(t *testing.T) {
	idx, raw := buildBrokenGraphIndex(t)
	var id int64
	require.NoError(t, raw.QueryRow(`SELECT id FROM symbols WHERE name = 'helper'`).Scan(&id))
	_, err := raw.Exec(`INSERT INTO edges (source_id, target_id, kind) VALUES (?, ?, 'calls')`, 987654321, id)
	require.NoError(t, err)

	res, err := NewCodeGraphTool(idx).Execute(context.Background(), map[string]any{"symbol": "helper", "direction": "callers"}, nil)
	require.NoError(t, err)
	assert.True(t, res.Error, res.Content)
	assert.Contains(t, res.Content, "987654321")
}
