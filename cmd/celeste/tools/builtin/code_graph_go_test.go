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

// buildGoIndex indexes a small Go module with an interface and a package
// that does not type-check (#375).
func buildGoIndex(t *testing.T) *codegraph.Indexer {
	t.Helper()
	ws := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"iface/iface.go": `package iface

type Doer interface{ Do() }

type Impl struct{}

func (Impl) Do() {}

func Call(d Doer) { d.Do() }
`,
		"broken/broken.go": `package broken

func Broken() { missing() }
`,
	}
	for name, body := range files {
		p := filepath.Join(ws, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	idx, err := codegraph.NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	return idx
}

func TestCodeGraphTool_GoInterfaceAndApproximate(t *testing.T) {
	idx := buildGoIndex(t)
	tool := NewCodeGraphTool(idx)

	res, err := tool.Execute(context.Background(), map[string]any{"symbol": "Do", "direction": "both"}, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "## (iface.Impl).Do (method)")
	assert.Contains(t, res.Content, "Implements: iface.Doer")
	assert.Contains(t, res.Content, "<- (iface.Doer).Do (implements)")
	assert.Contains(t, res.Content, "## (iface.Doer).Do (interface_method)")
	assert.Contains(t, res.Content, "<- Call (calls)")
	assert.NotContains(t, res.Content, "approximate")

	res, err = tool.Execute(context.Background(), map[string]any{"symbol": "Broken"}, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "approximate: this file did not type-check")
}

func TestCodeSymbolsTool_ListsInterfaceMethods(t *testing.T) {
	idx := buildGoIndex(t)
	res, err := NewCodeSymbolsTool(idx).Execute(context.Background(), map[string]any{"file": filepath.FromSlash("iface/iface.go")}, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "### Interface methods (1)")
	assert.Contains(t, res.Content, "func (Doer) Do()")
}
