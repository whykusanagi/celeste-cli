//go:build cgo

package codegraph

import (
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Review of #406: a non-Go method is found by its class: Class.method,
// Class::method and file.Class.method name the method of that class, not a
// same-named function or a same-named method of another class in the file.
func TestLookupSymbol_NonGoClassMethod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "calc.py", "class Foo:\n    def add(self, a):\n        return a\n\nclass Other:\n    def add(self, a):\n        return a\n\ndef add(x):\n    return x\n")
	writeFile(t, dir, "calc.ts", "export class Bar {\n  add(a: number) { return a }\n}\nexport class Other {\n  add(a: number) { return a }\n}\n")
	writeFile(t, dir, "calc.php", "<?php\nclass Baz {\n  public function add($a) { return $a; }\n}\nclass Other {\n  public function add($a) { return $a; }\n}\n")
	writeFile(t, dir, "calc.rb", "module Geo\n  class Qux\n    def add(a)\n      a\n    end\n  end\nend\nclass Other\n  def add(a)\n    a\n  end\nend\n")
	writeFile(t, dir, "calc.cpp", "class Cpp {\npublic:\n  int add(int a) { return a; }\n};\nclass Other {\npublic:\n  int add(int a) { return a; }\n};\nint Cpp2::mul(int a) { return a; }\n")
	writeFile(t, dir, "Calc.java", "public class Jv {\n  public int add(int a) { return a; }\n}\nclass Other {\n  int add(int a) { return a; }\n}\n")
	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	lookup := func(q string) (MatchKind, []string) {
		t.Helper()
		res, err := idx.LookupSymbol(q)
		require.NoError(t, err)
		var got []string
		for _, s := range res.Symbols {
			got = append(got, s.File+":"+itoa(s.Line))
		}
		sort.Strings(got)
		return res.Match, got
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"Foo.add", []string{"calc.py:2"}},
		{"Foo::add", []string{"calc.py:2"}},
		{"calc.Foo.add", []string{"calc.py:2"}},
		{"Other.add", []string{"Calc.java:5", "calc.cpp:7", "calc.php:6", "calc.py:6", "calc.rb:9", "calc.ts:5"}},
		{"calc.Other.add", []string{"calc.cpp:7", "calc.php:6", "calc.py:6", "calc.rb:9", "calc.ts:5"}},
		{"Bar.add", []string{"calc.ts:2"}},
		{"Baz.add", []string{"calc.php:3"}},
		{"Baz::add", []string{"calc.php:3"}},
		{"Qux.add", []string{"calc.rb:3"}},
		{"Geo::Qux.add", []string{"calc.rb:3"}},
		{"Geo::Qux::add", []string{"calc.rb:3"}},
		{"Cpp::add", []string{"calc.cpp:3"}},
		{"Cpp.add", []string{"calc.cpp:3"}},
		{"Cpp2::mul", []string{"calc.cpp:9"}},
		{"Cpp2.mul", []string{"calc.cpp:9"}},
		{"Jv.add", []string{"Calc.java:2"}},
	} {
		match, got := lookup(tc.query)
		assert.Contains(t, []MatchKind{MatchExact, MatchQualified}, match, tc.query)
		assert.Equal(t, tc.want, got, tc.query)
	}

	// The plain name still finds every add; the file qualifier still finds
	// the file's adds.
	_, got := lookup("add")
	assert.Len(t, got, 13)
	_, got = lookup("calc.add")
	assert.Contains(t, got, "calc.py:9")
	// A class that does not declare the method matches nothing qualified.
	match, _ := lookup("Jv.sub")
	assert.NotEqual(t, MatchQualified, match)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// The printed qualified name of a non-Go method carries its class, and
// LookupSymbol takes it back to that one method.
func TestQualifiedNames_NonGoMethodRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pkg/calc.py", "class Foo:\n    def add(self, a):\n        return a\n\nclass Other:\n    def add(self, a):\n        return a\n\ndef add(x):\n    return x\n")
	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())
	res, err := idx.LookupSymbol("add")
	require.NoError(t, err)
	require.Len(t, res.Symbols, 3)
	names := QualifiedNames(res.Symbols)
	assert.ElementsMatch(t, []string{"calc.Foo.add", "calc.Other.add", "calc.add"}, names)
	for i, n := range names {
		back, err := idx.LookupSymbol(n)
		require.NoError(t, err)
		require.Len(t, back.Symbols, 1, n)
		assert.Equal(t, res.Symbols[i].ID, back.Symbols[0].ID, n)
	}
}

// Review of the class-method lookup: a C++ member defined outside its class
// is printed with its file and its "Class::member" name
// ("a/shape.Shape::area" when two files define it), and LookupSymbol takes
// that name back to the one definition.
func TestQualifiedNames_CppOutOfLineMemberRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a/shape.cpp", "int Shape::area() { return 1; }\n")
	writeFile(t, dir, "b/shape.cpp", "int Shape::area() { return 2; }\n")
	writeFile(t, dir, "solo.cpp", "int Solo::area() { return 3; }\n")
	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())
	res, err := idx.LookupSymbol("Shape::area")
	require.NoError(t, err)
	require.Len(t, res.Symbols, 2)
	names := QualifiedNames(res.Symbols)
	assert.ElementsMatch(t, []string{"a/shape.Shape::area", "b/shape.Shape::area"}, names)
	for i, n := range names {
		back, err := idx.LookupSymbol(n)
		require.NoError(t, err)
		assert.Equal(t, MatchQualified, back.Match, n)
		require.Len(t, back.Symbols, 1, n)
		assert.Equal(t, res.Symbols[i].ID, back.Symbols[0].ID, n)
	}
	solo, err := idx.LookupSymbol("Solo::area")
	require.NoError(t, err)
	require.Len(t, solo.Symbols, 1)
	name := QualifiedName(solo.Symbols[0])
	assert.Equal(t, "solo.Solo::area", name)
	back, err := idx.LookupSymbol(name)
	require.NoError(t, err)
	require.Len(t, back.Symbols, 1, name)
	assert.Equal(t, solo.Symbols[0].ID, back.Symbols[0].ID)
}

// A C++ namespace is not part of a member's scope (docs/CODEGRAPH.md): a
// member of geo::Pt is found by its class, alone or after its file.
func TestLookupSymbol_CppClassInNamespace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pt.cpp", "namespace geo {\nclass Pt {\npublic:\n  int x() { return 1; }\n};\n}\n")
	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())
	for _, q := range []string{"Pt::x", "Pt.x", "pt.Pt.x"} {
		res, err := idx.LookupSymbol(q)
		require.NoError(t, err)
		assert.Equal(t, MatchQualified, res.Match, q)
		require.Len(t, res.Symbols, 1, q)
		assert.Equal(t, 4, res.Symbols[0].Line, q)
		assert.Equal(t, "pt.Pt.x", QualifiedName(res.Symbols[0]))
	}
}

// Review of the class-method lookup: a method of `impl Trait for Type` is
// scoped by the type, not the trait, so two trait impls in one file keep
// their methods apart and Type::method finds the right one.
func TestLookupSymbol_RustTraitImplScopedByType(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "lib.rs", "struct A;\nstruct B;\nstruct Foo<T>(T);\n"+
		"impl Display for A {\n    fn fmt(&self) {}\n}\n"+
		"impl Display for B {\n    fn fmt(&self) {}\n}\n"+
		"impl<T> T2 for Foo<T> {\n    fn go(&self) {}\n}\n"+
		"impl fmt::Debug for geo::Pt {\n    fn dbg(&self) {}\n}\n")
	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	res, err := idx.LookupSymbol("fmt")
	require.NoError(t, err)
	require.Len(t, res.Symbols, 2, "each impl's fmt keeps its own row")
	assert.ElementsMatch(t, []string{"lib.A.fmt", "lib.B.fmt"}, QualifiedNames(res.Symbols))
	for q, line := range map[string]int{"A::fmt": 5, "B.fmt": 8, "Foo::go": 11, "lib.Foo.go": 11, "Pt::dbg": 14} {
		res, err := idx.LookupSymbol(q)
		require.NoError(t, err)
		assert.Equal(t, MatchQualified, res.Match, q)
		require.Len(t, res.Symbols, 1, q)
		assert.Equal(t, line, res.Symbols[0].Line, q)
	}
}
