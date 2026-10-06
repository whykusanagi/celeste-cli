//go:build cgo

package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A C++ call through a qualified name (ns::f(), Cls::f(), ns::a::b(),
// ::f(), ns::f<T>()) is a call_expression whose function is a
// qualified_identifier, and a call to a template is a template_function.
// Each must produce an edge to the callee's own name, the name the
// declaration is indexed under (#397).
func TestMultiLangParser_CppQualifiedCalls(t *testing.T) {
	src := `namespace geo {
double totalArea() { return 1; }
namespace a { void b() {} }
template <typename T> T pick() { return T(); }
}
struct Shape { static int make() { return 1; } };
void globalFn() {}

void runShapes() {
    geo::totalArea();
    Shape::make();
    geo::a::b();
    ::globalFn();
    geo::pick<int>();
    std::vector<int>::size();
    pickLocal<int>();
}
`
	path := writeTempFile(t, "shapes.cpp", src)
	p := NewMultiLangParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	for _, target := range []string{"totalArea", "make", "b", "globalFn", "pick", "size", "pickLocal"} {
		assert.Contains(t, result.Edges, RawEdge{SourceName: "runShapes", TargetName: target, Kind: EdgeCalls})
	}
}

// End to end: the indexer stores the runShapes -> geo::totalArea edge
// that the 2.0 verification found missing (#397).
func TestIndexer_CppNamespacedCallEdge(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "geo.cpp", "namespace geo {\ndouble totalArea() { return 1; }\n}\n")
	writeFile(t, dir, "main.cpp", "void runShapes() { geo::totalArea(); }\n")

	assertCallEdge(t, dir, "runShapes", "totalArea")
}

// assertCallEdge builds an index over dir and asserts a call edge from
// the symbol named caller to the symbol named callee.
func assertCallEdge(t *testing.T, dir, caller, callee string) {
	t.Helper()
	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	store := idx.Store()
	src, ok := store.GetSymbolIDByName(caller)
	require.Truef(t, ok, "symbol %s not indexed", caller)
	dst, ok := store.GetSymbolIDByName(callee)
	require.Truef(t, ok, "symbol %s not indexed", callee)
	edges, err := store.GetEdgesFrom(src)
	require.NoError(t, err)
	for _, e := range edges {
		if e.TargetID == dst && e.Kind == EdgeCalls {
			return
		}
	}
	t.Errorf("no call edge %s -> %s", caller, callee)
}
