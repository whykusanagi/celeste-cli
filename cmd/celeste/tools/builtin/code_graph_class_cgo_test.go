//go:build cgo

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// Review of #406: code_graph finds a non-Go method by Class.method or
// Class::method, and details only that class's method.
func TestCodeGraphTool_NonGoClassMethod(t *testing.T) {
	ws := t.TempDir()
	files := map[string]string{
		"calc.py": "class Foo:\n    def add(self, a):\n        return helper(a)\n\nclass Other:\n    def add(self, a):\n        return a\n\ndef helper(x):\n    return x\n\ndef add(x):\n    return x\n",
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644))
	}
	idx, err := codegraph.NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	tool := NewCodeGraphTool(idx)

	for _, q := range []string{"Foo.add", "Foo::add", "calc.Foo.add"} {
		res, err := tool.Execute(context.Background(), map[string]any{"symbol": q, "direction": "callees"}, nil)
		require.NoError(t, err)
		require.False(t, res.Error, res.Content)
		assert.Contains(t, res.Content, "## add (function) — calc.py:2", q)
		assert.NotContains(t, res.Content, "calc.py:6", q)
		assert.NotContains(t, res.Content, "calc.py:12", q)
		assert.Contains(t, res.Content, "-> helper", q)
	}

	res, err := tool.Execute(context.Background(), map[string]any{"symbol": "add"}, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "calc.Foo.add")
	assert.Contains(t, res.Content, "calc.Other.add")
}
