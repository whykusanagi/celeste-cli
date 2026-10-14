package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/gittest"
)

func TestAnalyzeChangesIncludesTransitiveCallers(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{
			name: "three level caller chain",
			want: []string{"C", "B", "A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			gittest.Run(t, dir, "init", "-q")

			sourcePath := filepath.Join(dir, "chain.go")
			initial := "package p\n\nfunc A() { B() }\nfunc B() { C() }\nfunc C() { D() }\nfunc D() {}\n"
			require.NoError(t, os.WriteFile(sourcePath, []byte(initial), 0o644))
			gittest.Run(t, dir, "add", "chain.go")
			gittest.Run(t, dir, "commit", "-q", "-m", "initial")

			changed := "package p\n\nfunc A() { B() }\nfunc B() { C() }\nfunc C() { D() }\nfunc D() { println(\"changed\") }\n"
			require.NoError(t, os.WriteFile(sourcePath, []byte(changed), 0o644))

			store := newTestStore(t)
			defer store.Close()
			idx := NewIndexerWithStore(store, dir)

			ids := make(map[string]int64)
			for _, sym := range []Symbol{
				{Name: "A", Kind: SymbolFunction, File: "chain.go", Line: 3},
				{Name: "B", Kind: SymbolFunction, File: "chain.go", Line: 4},
				{Name: "C", Kind: SymbolFunction, File: "chain.go", Line: 5},
				{Name: "D", Kind: SymbolFunction, File: "chain.go", Line: 6},
			} {
				id, err := store.UpsertSymbol(sym)
				require.NoError(t, err)
				ids[sym.Name] = id
			}

			require.NoError(t, store.AddEdge(ids["A"], ids["B"], EdgeCalls))
			require.NoError(t, store.AddEdge(ids["B"], ids["C"], EdgeCalls))
			require.NoError(t, store.AddEdge(ids["C"], ids["D"], EdgeCalls))

			result, err := idx.AnalyzeChanges("HEAD")
			require.NoError(t, err)

			got := make([]string, 0, len(result.AffectedCallers))
			for _, caller := range result.AffectedCallers {
				got = append(got, caller.Name)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAnalyzeChangesKeysCallersByID(t *testing.T) {
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-q")

	files := map[string]string{
		"d.go": "package p\n\nfunc D() {}\n",
		"a.go": "package p\n\nfunc Foo() { D() }\n",
		"b.go": "package p\n\nfunc Foo() { Unrelated() }\n",
		"c.go": "package p\n\nfunc Foo() { D() }\n",
		"u.go": "package p\n\nfunc Unrelated() {}\n",
		"z.go": "package p\n\nfunc Z() { Foo() }\n",
	}
	for name, contents := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644))
	}
	gittest.Run(t, dir, "add", "d.go", "a.go", "b.go", "c.go", "u.go", "z.go")
	gittest.Run(t, dir, "commit", "-q", "-m", "initial")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "d.go"), []byte("package p\n\nfunc D() { println(\"changed\") }\n"), 0o644))

	store := newTestStore(t)
	defer store.Close()
	idx := NewIndexerWithStore(store, dir)

	dID, err := store.UpsertSymbol(Symbol{Name: "D", Kind: SymbolFunction, File: "d.go", Line: 3})
	require.NoError(t, err)
	fooAID, err := store.UpsertSymbol(Symbol{Name: "Foo", Kind: SymbolFunction, File: "a.go", Line: 3})
	require.NoError(t, err)
	fooBID, err := store.UpsertSymbol(Symbol{Name: "Foo", Kind: SymbolFunction, File: "b.go", Line: 3})
	require.NoError(t, err)
	fooCID, err := store.UpsertSymbol(Symbol{Name: "Foo", Kind: SymbolFunction, File: "c.go", Line: 3})
	require.NoError(t, err)
	unrelatedID, err := store.UpsertSymbol(Symbol{Name: "Unrelated", Kind: SymbolFunction, File: "u.go", Line: 3})
	require.NoError(t, err)
	zID, err := store.UpsertSymbol(Symbol{Name: "Z", Kind: SymbolFunction, File: "z.go", Line: 3})
	require.NoError(t, err)

	require.NoError(t, store.AddEdge(fooAID, dID, EdgeCalls))
	require.NoError(t, store.AddEdge(fooCID, dID, EdgeCalls))
	require.NoError(t, store.AddEdge(fooBID, unrelatedID, EdgeCalls))
	require.NoError(t, store.AddEdge(zID, fooBID, EdgeCalls))

	result, err := idx.AnalyzeChanges("HEAD")
	require.NoError(t, err)

	got := make([]struct {
		Name string
		File string
	}, 0, len(result.AffectedCallers))
	for _, caller := range result.AffectedCallers {
		got = append(got, struct {
			Name string
			File string
		}{
			Name: caller.Name,
			File: caller.File,
		})
	}
	assert.ElementsMatch(t, []struct {
		Name string
		File string
	}{
		{Name: "Foo", File: "a.go"},
		{Name: "Foo", File: "c.go"},
	}, got)
}
