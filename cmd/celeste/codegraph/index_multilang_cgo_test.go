//go:build cgo

package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tree-sitter grammars for Java, C, C++ and Ruby ship in every cgo
// build, so the indexer must hand those files to them. Each file below
// declares a caller and a callee; the graph needs both symbols and the
// call edge between them.
func TestIndexer_IndexesJavaCCppRuby(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "A.java", "class A {\n  void runJava() { helperJava(); }\n  void helperJava() {}\n}\n")
	writeFile(t, dir, "b.rb", "def run_ruby\n  helper_ruby\n  helper_ruby()\nend\n\ndef helper_ruby\nend\n")
	writeFile(t, dir, "c.c", "void helper_c(void) {}\nvoid run_c(void) { helper_c(); }\n")
	writeFile(t, dir, "d.cpp", "void helper_cpp() {}\nvoid run_cpp() { helper_cpp(); }\n")
	writeFile(t, dir, "e.cxx", "void helper_cxx() {}\nvoid run_cxx() { helper_cxx(); }\n")

	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	store := idx.Store()
	for _, pair := range [][2]string{
		{"runJava", "helperJava"},
		{"run_ruby", "helper_ruby"},
		{"run_c", "helper_c"},
		{"run_cpp", "helper_cpp"},
		{"run_cxx", "helper_cxx"},
	} {
		caller, ok := uniqueSymbolID(store, pair[0])
		if !assert.Truef(t, ok, "symbol %s not indexed", pair[0]) {
			continue
		}
		callee, ok := uniqueSymbolID(store, pair[1])
		if !assert.Truef(t, ok, "symbol %s not indexed", pair[1]) {
			continue
		}
		edges, err := store.GetEdgesFrom(caller)
		require.NoError(t, err)
		found := false
		for _, e := range edges {
			if e.TargetID == callee && e.Kind == EdgeCalls {
				found = true
			}
		}
		assert.Truef(t, found, "no call edge %s -> %s", pair[0], pair[1])
	}
}

// uniqueSymbolID is the ID of the one symbol with this name.
func uniqueSymbolID(s *Store, name string) (int64, bool) {
	res, err := s.LookupSymbol(name)
	if err != nil || res.Match != MatchExact || len(res.Symbols) != 1 {
		return 0, false
	}
	return res.Symbols[0].ID, true
}
