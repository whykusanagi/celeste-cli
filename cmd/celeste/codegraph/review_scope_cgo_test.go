//go:build cgo

package codegraph

import "testing"

// #396 fixtures for the tree-sitter languages: exact findings per language.
var treeSitterReviewCases = []reviewCase{
	{
		lang:   "typescript",
		scoped: []string{"TODO_FIXME app.ts:reset:25", "PLACEHOLDER app.ts:apiEntry:31"},
		stubs: []string{
			"STUB app.ts:reset:24 dead",
			"STUB app.ts:neverCalled:28 dead",
			"STUB app.ts:apiEntry:31 live",
		},
	},
	{
		lang:   "php",
		scoped: []string{"TODO_FIXME app.php:checkout:26"},
		stubs: []string{
			"STUB app.php:checkout:24 dead",
			"STUB app.php:deadFunction:30 dead",
		},
	},
	{
		lang:   "python",
		scoped: []string{"TODO_FIXME app.py:search:9", "HARDCODED app.py:run:30"},
		stubs: []string{
			"STUB app.py:search:8 dead",
			"STUB app.py:never_used:21 dead",
			"STUB app.py:not_done:25 dead",
		},
	},
	{
		lang:   "java",
		scoped: []string{"TODO_FIXME App.java:later:13"},
		stubs: []string{
			"STUB App.java:unusedMethod:9 dead",
			"STUB App.java:later:12 dead",
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
		scoped: []string{},
		stubs:  []string{"STUB shapes.cpp:unusedCpp:12 dead"},
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
