//go:build cgo

package codegraph

import (
	"strings"
	"testing"
)

// #396 fixtures for the tree-sitter languages: exact findings per language.
var treeSitterReviewCases = []reviewCase{
	{
		lang:   "typescript",
		scoped: []string{"TODO_FIXME app.ts:reset:25", "PLACEHOLDER app.ts:apiEntry:31", "TODO_FIXME shapes.ts:close:7"},
		stubs: []string{
			"STUB shapes.ts:close:6 live",
			// Public methods of an exported class are library API.
			"STUB svc.ts:ping:2 live",
			"STUB svc.ts:hidden:4 dead",
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
		scoped: []string{"TODO_FIXME app.py:search:9", "HARDCODED app.py:run:30", "TODO_FIXME print_job.py:run_job:6", "TODO_FIXME app.py:handler:38"},
		stubs: []string{
			// A framework calls what its decorator registers.
			"STUB app.py:handler:37 live",
			"STUB app.py:unused_static:44 dead",
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
		stubs: []string{
			"STUB shapes.cpp:unusedCpp:12 dead",
			"STUB shapes.cpp:sides:21 live",
			// Methods defined outside their class (.hpp/.h + .cpp).
			"STUB shape.cpp:D::f:3 live",
			"STUB shape.cpp:D::g:5 dead",
			"STUB widget.cpp:Button::draw:3 live",
			"STUB widget.cpp:Button::paintEvent:5 live",
		},
	},
	{
		lang: "rust",
		stubs: []string{
			"STUB main.rs:speak:8 live",
			"STUB main.rs:drop:12 live",
			"STUB main.rs:dead_rs:15 dead",
		},
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

// #405 review: a same-named method in an unrelated class never stands in
// for a base class or interface. Overriding, implementing and the
// abstract-declaration rule all need the method's own class to extend or
// implement the other class (directly or through its bases).
func TestReview_UnrelatedSameNameClass(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			// Unrelated.run only raises "not implemented"; Child overrides
			// Base.run, not Unrelated.run, so Unrelated.run is still a STUB.
			// Shape.area is the abstract declaration Circle overrides,
			// through Mid, which has no methods of its own.
			name: "python",
			files: map[string]string{"app.py": `class Base:
    def run(self):
        return 1


class Child(Base):
    def run(self):
        return 2


class Unrelated:
    def run(self):
        raise NotImplementedError


class Shape:
    def area(self):
        raise NotImplementedError


class Mid(Shape):
    pass


class Circle(Mid):
    def area(self):
        return 3
`},
			want: []string{"STUB app.py:run:12 dead"},
		},
		{
			// Door extends a class but not Closer: its empty close
			// implements nothing. Pipe implements Closer.
			name: "java",
			files: map[string]string{
				"Closer.java": "interface Closer {\n    void close();\n}\n",
				"Door.java":   "class Frame {}\n\nclass Door extends Frame {\n    void close() {}\n}\n",
				"Pipe.java":   "class Pipe implements Closer {\n    public void close() {}\n}\n",
			},
			want: []string{"STUB Door.java:close:4 dead", "STUB Pipe.java:close:2 live"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			idx, _ := buildFixture(t, c.files)
			smells, err := idx.FindCodeSmells(nil, 1000, false)
			if err != nil {
				t.Fatal(err)
			}
			got := smellKeys(smells, true, SmellStub)
			if strings.Join(got, "\n") != strings.Join(sorted(c.want...), "\n") {
				t.Errorf("STUB rows:\n got %q\nwant %q", got, sorted(c.want...))
			}
		})
	}
}
