//go:build cgo

package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ruby has no import syntax, so the "call" node type is both its import
// and its call type. Only require-style calls are imports; every other
// call must produce an edge.
func TestMultiLangParser_RubyCallsAndRequires(t *testing.T) {
	src := "require 'json'\nrequire_relative \"lib/helper\"\n\nclass Runner\n  def run\n    prepare()\n    @store.save(1)\n  end\n\n  def prepare\n  end\nend\n"
	path := writeTempFile(t, "runner.rb", src)
	p := NewMultiLangParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	imports := map[string]bool{}
	kinds := map[string]SymbolKind{}
	for _, s := range result.Symbols {
		if s.Kind == SymbolImport {
			imports[s.Name] = true
		} else {
			kinds[s.Name] = s.Kind
		}
	}
	assert.Equal(t, map[string]bool{"json": true, "lib/helper": true}, imports)
	assert.Equal(t, SymbolClass, kinds["Runner"])
	assert.Contains(t, kinds, "run")
	assert.Contains(t, kinds, "prepare")
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "run", TargetName: "prepare", Kind: EdgeCalls})
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "run", TargetName: "save", Kind: EdgeCalls})
}
