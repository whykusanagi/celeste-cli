//go:build cgo

package codegraph

import "testing"

// #396 fixtures for the tree-sitter languages: exact findings per language.
var treeSitterReviewCases = []reviewCase{
	{
		lang:   "typescript",
		scoped: []string{"TODO_FIXME app.ts:reset:25", "PLACEHOLDER app.ts:apiEntry:31", "TODO_FIXME shapes.ts:close:7"},
		stubs: []string{
			"STUB shapes.ts:close:6 live",
			"STUB app.ts:reset:24 dead",
			"STUB app.ts:neverCalled:28 dead",
			"STUB app.ts:apiEntry:31 live",
		},
	},
	{
		lang:   "php",
		scoped: []string{"TODO_FIXME app.php:checkout:26", "TODO_FIXME canvas.php:draw:7"},
		stubs: []string{
			"STUB app.php:checkout:24 dead",
			"STUB app.php:deadFunction:30 dead",
			"STUB canvas.php:draw:5 live",
		},
	},
	{
		lang:   "python",
		scoped: []string{"TODO_FIXME app.py:search:9", "HARDCODED app.py:run:30", "TODO_FIXME print_job.py:run_job:6"},
		stubs: []string{
			"STUB app.py:search:8 dead",
			"STUB app.py:never_used:21 dead",
			"STUB app.py:not_done:25 dead",
			"STUB print_job.py:run_job:5 live",
		},
	},
	{
		lang:   "java",
		scoped: []string{"TODO_FIXME App.java:later:13", "TODO_FIXME Pipe.java:close:4", "TODO_FIXME Lib.java:api:3"},
		stubs: []string{
			"STUB Lib.java:api:2 live",
			"STUB Lib.java:hidden:6 dead",
			"STUB App.java:unusedMethod:9 dead",
			"STUB App.java:later:12 dead",
			"STUB Pipe.java:close:2 live",
		},
	},
	{
		lang:   "c",
		scoped: []string{"TODO_FIXME main.c:todo_c:10"},
		stubs: []string{
			"STUB main.c:todo_c:9 dead",
			"STUB main.c:dead_c:13 dead",
		},
	},
	{
		lang:   "cpp",
		scoped: []string{"TODO_FIXME shapes.cpp:sides:22"},
		stubs:  []string{"STUB shapes.cpp:unusedCpp:12 dead", "STUB shapes.cpp:sides:21 live"},
	},
	{
		lang: "ruby",
		scoped: []string{
			"TODO_FIXME bank.rb:withdraw:20",
			"PLACEHOLDER bank.rb:withdraw:19",
			"TODO_FIXME null.rb:initialize:3",
		},
		stubs: []string{
			"STUB bank.rb:withdraw:19 dead",
			"STUB bank.rb:dead_rb:24 dead",
			"STUB null.rb:initialize:2 live",
		},
	},
}

func TestReview_TreeSitterScoped(t *testing.T) {
	for _, c := range treeSitterReviewCases {
		t.Run(c.lang, func(t *testing.T) { runScopedCase(t, c) })
	}
}

func TestReview_TreeSitterStubs(t *testing.T) {
	for _, c := range treeSitterReviewCases {
		t.Run(c.lang, func(t *testing.T) { runStubCase(t, c) })
	}
}

// #396 G6 for the tree-sitter languages: constructors, interface and
// abstract declarations and their implementations, exported JS/TS API.
func TestReview_TreeSitterDeadCode(t *testing.T) {
	for _, c := range treeSitterReviewCases {
		t.Run(c.lang, func(t *testing.T) { runDeadCase(t, c) })
	}
}

func TestReview_CppConstructorNames(t *testing.T) {
	cases := map[[2]string]bool{
		{"Shape", "Shape"}:         true,
		{"~Shape", "Shape"}:        true,
		{"Shape::Shape", ""}:       true,
		{"geo::Shape::~Shape", ""}: true,
		{"area", "Shape"}:          false,
		{"geo::area", ""}:          false,
	}
	for in, want := range cases {
		if got := isCppConstructor(in[0], in[1]); got != want {
			t.Errorf("isCppConstructor(%q, %q) = %v, want %v", in[0], in[1], got, want)
		}
	}
}
