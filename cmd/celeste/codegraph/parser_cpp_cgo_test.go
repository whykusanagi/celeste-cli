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
// Each must produce an edge whose target is the qualified callee with
// any leading "::" and template arguments dropped; the indexer resolves
// it exactly ("Shape::make", an out-of-line definition) or by its last
// segment ("geo::totalArea", defined inside a namespace block) (#397).
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

	for _, target := range []string{"geo::totalArea", "Shape::make", "geo::a::b", "globalFn", "geo::pick", "std::vector::size", "pickLocal"} {
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
	src, ok := uniqueSymbolID(store, caller)
	require.Truef(t, ok, "symbol %s not indexed", caller)
	dst, ok := uniqueSymbolID(store, callee)
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

// A member defined out of line (int Shape::make() {...}) is indexed
// under its qualified name, without template arguments, so members of
// different classes defined in one .cpp stay distinct symbols and a
// Shape::make() call resolves to it exactly (#397).
func TestMultiLangParser_CppOutOfLineDefinitionName(t *testing.T) {
	src := `int Shape::make() { return helper(); }
Shape::~Shape() {}
int* geo::a::ptr() { return 0; }
const Shape& Shape::self() const { return *this; }
template <typename T> void Box<T>::put() {}
`
	path := writeTempFile(t, "shape.cpp", src)
	p := NewMultiLangParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, s := range result.Symbols {
		names[s.Name] = true
	}
	for _, want := range []string{"Shape::make", "Shape::~Shape", "geo::a::ptr", "Shape::self", "Box::put"} {
		assert.Truef(t, names[want], "symbol %q not extracted; got %v", want, names)
	}
	assert.Contains(t, result.Edges, RawEdge{SourceName: "Shape::make", TargetName: "helper", Kind: EdgeCalls})
}

func TestIndexer_CppStaticMethodOutOfLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shape.hpp", "struct Shape { static int make(); };\n")
	writeFile(t, dir, "shape.cpp", "int Shape::make() { return 1; }\n")
	writeFile(t, dir, "main.cpp", "void runShapes() { Shape::make(); }\n")

	assertCallEdge(t, dir, "runShapes", "Shape::make")
}

// Members of different classes with the same short name, defined out of
// line in one .cpp (Circle::draw next to Square::draw, a pimpl
// Shape::run next to Shape::Impl::run), stay distinct symbols with their
// own lines, and each call is credited to the member that makes it.
// Indexing them under the short name merged each pair into one row
// (#397 review).
func TestIndexer_CppSameFileMembersStayDistinct(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shapes.cpp", `void helperA() {}
void helperB() {}
void Circle::draw() { helperA(); }
void Square::draw() { helperB(); }
void Shape::Impl::run() {}
void Shape::run() { impl_->run(); Impl::run(); }
`)
	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())
	store := idx.Store()

	syms, err := store.GetSymbolsByFile("shapes.cpp")
	require.NoError(t, err)
	lines := map[string]int{}
	for _, s := range syms {
		lines[s.Name] = s.Line
	}
	assert.Equal(t, 3, lines["Circle::draw"], "symbols: %v", lines)
	assert.Equal(t, 4, lines["Square::draw"], "symbols: %v", lines)
	assert.Equal(t, 5, lines["Shape::Impl::run"], "symbols: %v", lines)
	assert.Equal(t, 6, lines["Shape::run"], "symbols: %v", lines)
	assert.NotContains(t, lines, "draw")
	assert.NotContains(t, lines, "run")

	callerNames := func(target string) map[string]int {
		out := map[string]int{}
		for _, c := range store.CallersOf(target) {
			out[c.Name] = c.Line
		}
		return out
	}
	assert.Equal(t, map[string]int{"Circle::draw": 3}, callerNames("helperA"))
	assert.Equal(t, map[string]int{"Square::draw": 4}, callerNames("helperB"))
	// Shape::run calls Impl::run, not itself: no run -> run self-loop.
	assert.Empty(t, callerNames("Shape::run"))
}
