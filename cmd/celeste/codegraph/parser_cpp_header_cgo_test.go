//go:build cgo

package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cppHeaderSrc = `#pragma once
namespace geo {
class Shape {
public:
    virtual ~Shape() {}
    virtual double area() const { return 0; }
    int sides() const { return count(); }
private:
    int count() const { return 0; }
};
}
`

const cHeaderSrc = `#ifndef UTIL_H
#define UTIL_H
#ifdef __cplusplus
extern "C" {
#endif
struct point { int x; int y; };
static inline int add(int a, int b) { return a + b; }
static inline int twice(int a) { return add(a, a); }
#ifdef __cplusplus
}
#endif
#endif
`

func symbolSummary(res *ParseResult) []string {
	var out []string
	for _, s := range res.Symbols {
		out = append(out, string(s.Kind)+" "+s.Name+" "+s.QualName+" "+s.Package)
	}
	return out
}

func parseAs(t *testing.T, name, src string) *ParseResult {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	p := NewMultiLangParser()
	defer p.Close()
	res, err := p.ParseFile(path)
	require.NoError(t, err)
	return res
}

// Review of #381: a .h header holding C++ is parsed with the C++ grammar,
// so its classes and their members are found as in a .hpp; a plain C
// header stays on the C grammar.
func TestMultiLangParser_CppInDotHHeader(t *testing.T) {
	hpp := parseAs(t, "shape.hpp", cppHeaderSrc)
	h := parseAs(t, "shape.h", cppHeaderSrc)
	require.Contains(t, symbolSummary(hpp), "class Shape  ", "fixture: %v", symbolSummary(hpp))
	assert.Equal(t, symbolSummary(hpp), symbolSummary(h), "a C++ .h parses as its .hpp twin")
	assert.Equal(t, hpp.Edges, h.Edges)

	c := parseAs(t, "util.c", cHeaderSrc)
	ch := parseAs(t, "util.h", cHeaderSrc)
	require.NotEmpty(t, c.Symbols)
	assert.Equal(t, symbolSummary(c), symbolSummary(ch), "a C header parses as C")
	assert.Equal(t, c.Edges, ch.Edges)
}

// End to end: the indexer stores the class and member symbols of a C++
// header and its call edges.
func TestIndexer_CppDotHHeaderClasses(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shape.h", cppHeaderSrc)
	writeFile(t, dir, "main.cpp", "#include \"shape.h\"\nint run() { geo::Shape s; return s.sides(); }\n")
	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	syms, err := idx.store.GetSymbolsByFile("shape.h")
	require.NoError(t, err)
	kinds := map[string]SymbolKind{}
	for _, s := range syms {
		kinds[s.Name] = s.Kind
	}
	assert.Equal(t, SymbolClass, kinds["Shape"], "symbols: %v", syms)
	assert.Contains(t, kinds, "sides", "symbols: %v", syms)
	assert.True(t, edgeKeys(t, idx)["sides -calls-> count"], "edges: %v", edgeKeys(t, idx))
}
