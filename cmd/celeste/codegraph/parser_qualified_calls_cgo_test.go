//go:build cgo

package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Calls through a qualified name must produce an edge to the callee's
// own name in every tree-sitter language, the name its declaration is
// indexed under (#397). C++ is covered in parser_cpp_cgo_test.go.
func TestMultiLangParser_QualifiedCalls(t *testing.T) {
	cases := []struct {
		name, file, src, caller string
		targets                 []string
	}{
		{
			name: "rust paths", file: "lib.rs", caller: "run",
			src: "mod geo { pub mod a { pub fn b() {} } pub fn total() {} }\n" +
				"fn pick<T>() {}\n" +
				"fn run() {\n    geo::total();\n    crate::geo::a::b();\n    Vec::<i32>::new();\n    pick::<i32>();\n    <Shape as Area>::area();\n}\n",
			targets: []string{"total", "b", "new", "pick", "area"},
		},
		{
			name: "php static calls", file: "run.php", caller: "run",
			src:     "<?php\nfunction run() {\n    Cls::make();\n    \\App\\Geo::total();\n    static::build();\n    parent::init();\n}\n",
			targets: []string{"make", "total", "build", "init"},
		},
		{
			name: "ruby scoped calls", file: "run.rb", caller: "run",
			src:     "def run\n  Geo::total\n  Geo.area\n  Geo::make(1)\n  A::B.deep\n  A::B::deeper()\nend\n",
			targets: []string{"total", "area", "make", "deep", "deeper"},
		},
		{
			name: "java static calls", file: "Run.java", caller: "run",
			src:     "class Run {\n  void run() {\n    Util.helper();\n    java.util.Objects.hash(1);\n    Run.<String>typed();\n  }\n}\n",
			targets: []string{"helper", "hash", "typed"},
		},
	}
	p := NewMultiLangParser()
	defer p.Close()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := p.ParseFile(writeTempFile(t, tc.file, tc.src))
			require.NoError(t, err)
			for _, target := range tc.targets {
				assert.Contains(t, result.Edges, RawEdge{SourceName: tc.caller, TargetName: target, Kind: EdgeCalls})
			}
		})
	}
}

// End to end for each language: the indexer stores the edge from a
// caller to a function it reaches through a qualified name (#397).
func TestIndexer_QualifiedCallEdges(t *testing.T) {
	cases := []struct {
		name, file, src, caller, callee string
	}{
		{"rust", "lib.rs", "mod geo { pub fn total_area() {} }\nfn run_rust() { geo::total_area(); }\n", "run_rust", "total_area"},
		{"php", "run.php", "<?php\nclass Geo { public static function totalPhp() {} }\nfunction runPhp() { Geo::totalPhp(); }\n", "runPhp", "totalPhp"},
		{"ruby", "run.rb", "module Geo\n  def self.total_rb\n  end\nend\n\ndef run_rb\n  Geo::total_rb\nend\n", "run_rb", "total_rb"},
		{"java", "Run.java", "class Run {\n  static void totalJava() {}\n  void runJava() { Run.totalJava(); }\n}\n", "runJava", "totalJava"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tc.file, tc.src)
			assertCallEdge(t, dir, tc.caller, tc.callee)
		})
	}
}
