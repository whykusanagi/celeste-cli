package codegraph

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewFixtureCache keeps each fixture's findings: the scoped, stub and
// dead-code tests read the same review, so each fixture is indexed once.
// The tests using it do not run in parallel.
var reviewFixtureCache = map[string][]CodeSmell{}

// reviewFixture copies testdata/review/<lang> into a temp workspace, indexes
// it and returns every code smell, tests excluded.
func reviewFixture(t *testing.T, lang string) []CodeSmell {
	t.Helper()
	if smells, ok := reviewFixtureCache[lang]; ok {
		return smells
	}
	src := filepath.Join("testdata", "review", lang)
	ws := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	require.NoError(t, err)
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	smells, err := idx.FindCodeSmells(nil, 1000, false)
	require.NoError(t, err)
	reviewFixtureCache[lang] = smells
	return smells
}

// smellKeys renders the smells of the given kinds as "KIND file:name:line",
// sorted. withDead appends " dead" or " live" to STUB rows, by whether the
// reason calls the function likely dead code.
func smellKeys(smells []CodeSmell, withDead bool, kinds ...CodeSmellKind) []string {
	want := map[CodeSmellKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	keys := []string{}
	for _, s := range smells {
		if !want[s.Kind] {
			continue
		}
		k := fmt.Sprintf("%s %s:%s:%d", s.Kind, path.Base(filepath.ToSlash(s.File)), s.Name, s.Line)
		if withDead && s.Kind == SmellStub {
			if strings.Contains(s.Reason, "likely dead code") {
				k += " dead"
			} else {
				k += " live"
			}
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sorted(keys ...string) []string {
	if keys == nil {
		keys = []string{}
	}
	sort.Strings(keys)
	return keys
}

// reviewCase is one language fixture and the exact findings expected of it.
type reviewCase struct {
	lang string
	// scoped are the TODO_FIXME, PLACEHOLDER and HARDCODED rows (#396 G3, G4).
	scoped []string
	// stubs are the STUB rows, with dead/live (#396 G5, G6).
	stubs []string
}

var goReviewCase = reviewCase{
	lang: "go",
	scoped: []string{
		"TODO_FIXME main.go:Flush:26",
		"HARDCODED main.go:endpoint:32",
		"TODO_FIXME main.go:init:37",
		"PLACEHOLDER store.go:Export:15",
	},
	stubs: []string{
		"STUB main.go:unusedHelper:20 dead",
		"STUB main.go:Flush:25 dead",
		"STUB main.go:init:36 live",
		"STUB store.go:Export:15 live",
		"STUB store.go:unexportedDead:19 dead",
		"STUB store_windows.go:platformHook:3 live",
	},
}

func runScopedCase(t *testing.T, c reviewCase) {
	t.Helper()
	smells := reviewFixture(t, c.lang)
	assert.Equal(t, sorted(c.scoped...), smellKeys(smells, false, SmellTodoFixme, SmellPlaceholder, SmellHardcoded), "scoped findings")
	assert.Empty(t, smellKeys(smells, false, SmellLazyRedirect, SmellEmptyHandler))
}

// stripDead drops the " dead"/" live" suffix of STUB keys.
func stripDead(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.TrimSuffix(strings.TrimSuffix(k, " dead"), " live")
	}
	sort.Strings(out)
	return out
}

func runStubCase(t *testing.T, c reviewCase) {
	t.Helper()
	smells := reviewFixture(t, c.lang)
	assert.Equal(t, stripDead(c.stubs), stripDead(smellKeys(smells, false, SmellStub)), "STUB rows")
}

// #396 G5: STUB is exactly a function with no callers whose body is empty,
// holds only comments, or only raises "not implemented".
func TestReview_GoStubs(t *testing.T) {
	requireGoToolchain(t)
	runStubCase(t, goReviewCase)
}

func runDeadCase(t *testing.T, c reviewCase) {
	t.Helper()
	smells := reviewFixture(t, c.lang)
	assert.Equal(t, sorted(c.stubs...), smellKeys(smells, true, SmellStub), "STUB rows, dead or live")
}

// #396 G6: "likely dead code" is never said of code reached implicitly:
// init/main, an interface implementation, the exported API of a library
// package, a build-constrained file.
func TestReview_GoDeadCode(t *testing.T) {
	requireGoToolchain(t)
	runDeadCase(t, goReviewCase)
}

// #396 G5 regression on this repository: TrustPath (hooks/trust.go), a
// one-liner with a real body, is not a STUB.
func TestReview_RepoTrustPathIsNotAStub(t *testing.T) {
	smells, files := repoReview(t, "../hooks/trust.go")
	require.Contains(t, files["trust.go"], "func TrustPath(")
	for _, s := range smells {
		assert.False(t, s.Name == "TrustPath" && s.Kind == SmellStub, "TrustPath reported as a stub: %+v", s)
	}
}

// #396 G3, G4: every TODO_FIXME, PLACEHOLDER and HARDCODED finding is scoped
// to its own function body and carries the line it is on.
func TestReview_GoScoped(t *testing.T) {
	requireGoToolchain(t)
	runScopedCase(t, goReviewCase)
}

// repoReview indexes copies of some of this repository's own files and
// returns their code smells.
func repoReview(t *testing.T, rels ...string) ([]CodeSmell, map[string]string) {
	t.Helper()
	files := map[string]string{}
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.FromSlash(rel))
		require.NoError(t, err)
		files[path.Base(rel)] = string(data)
	}
	idx, _ := buildFixture(t, files)
	smells, err := idx.FindCodeSmells(nil, 1000, false)
	require.NoError(t, err)
	return smells, files
}

// lineOf returns the 1-based line of the first line of src with prefix.
func lineOf(t *testing.T, src, prefix string) int {
	t.Helper()
	for i, l := range strings.Split(src, "\n") {
		if strings.HasPrefix(l, prefix) {
			return i + 1
		}
	}
	t.Fatalf("no line starts with %q", prefix)
	return 0
}

// #396 regression on this repository: the one-line BaseTool methods
// (tools/builtin/base.go:27-36) no longer inherit Execute's "not
// implemented"; Execute's own PLACEHOLDER stays, on Execute's line.
func TestReview_RepoBaseToolPlaceholder(t *testing.T) {
	smells, files := repoReview(t, "../tools/builtin/base.go")
	src := files["base.go"]
	oneLiners := map[int]bool{}
	for i, l := range strings.Split(src, "\n") {
		if strings.HasPrefix(l, "func (b *BaseTool) ") && strings.HasSuffix(strings.TrimSpace(l), "}") {
			oneLiners[i+1] = true
		}
	}
	require.GreaterOrEqual(t, len(oneLiners), 7, "base.go keeps its one-line methods")
	execute := lineOf(t, src, "func (b *BaseTool) Execute(")
	assert.Equal(t, []string{fmt.Sprintf("PLACEHOLDER base.go:Execute:%d", execute)}, smellKeys(smells, false, SmellPlaceholder))
	for _, s := range smells {
		assert.False(t, oneLiners[s.Line] && s.Name != "Execute" && s.Kind != SmellStub, "finding on a one-line BaseTool method: %+v", s)
	}
}

// The text-scan fallback (CGO_ENABLED=0 builds, files no parser spans) ends
// each function at its own end, not at the next definition it recognises.
func TestReview_FallbackSpan(t *testing.T) {
	cases := []struct {
		name, lang, src string
		start, end      int
		hasBody         bool
		stmts, comments int
	}{
		{"ts method", "typescript", "class A {\n  push(x) {\n    this.a(x);\n  }\n\n  reset() {\n    // TODO: x\n  }\n}\n", 6, 8, true, 0, 1},
		{"one-liner", "typescript", "function add(a, b) { return a + b; }\nfunction b() {}\n", 1, 1, true, 1, 0},
		{"java interface method", "java", "interface S {\n    void speak();\n    void other();\n}\n", 2, 2, false, 0, 0},
		{"python", "python", "def a(x):\n    # TODO: y\n    pass\n\n\ndef b():\n    return 1\n", 1, 3, true, 1, 1},
		{"ruby", "ruby", "class A\n  def a\n    # TODO: y\n  end\n\n  def b\n    1\n  end\nend\n", 2, 4, true, 0, 1},
		{"ruby one-line", "ruby", "def a; end\ndef b\nend\n", 1, 1, true, 0, 0},
		{"c block comment", "c", "void f(void) {\n    /* TODO: finish\n       later */\n}\nint g(void) { return 1; }\n", 1, 4, true, 0, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fallbackSpan(strings.Split(c.src, "\n"), c.lang, c.start, "f")
			assert.Equal(t, c.end, s.End, "end")
			assert.Equal(t, c.hasBody, s.HasBody, "has body")
			assert.Len(t, s.Stmts, c.stmts, "stmts %q", s.Stmts)
			assert.Len(t, s.Comments, c.comments, "comments %q", s.Comments)
		})
	}
}

// A function nested in another keeps its own findings: the outer
// function's lines leave the nested function's lines out.
func TestReview_NestedLinesBelongToTheNestedFunction(t *testing.T) {
	f := &reviewFile{
		lines: strings.Split("def outer():\n    def inner():\n        # TODO: inner\n        pass\n    return inner\n", "\n"),
		spans: []funcSpan{{Name: "outer", Start: 1, End: 5}, {Name: "inner", Start: 2, End: 4}},
	}
	var got []int
	for _, l := range f.bodyLines(f.spans[0]) {
		got = append(got, l.n)
	}
	assert.Equal(t, []int{1, 5}, got)
	smells := detectTodoFixme(FunctionEdgeInfo{Name: "inner", File: "a.py"}, f.bodyLines(f.spans[1]))
	require.Len(t, smells, 1)
	assert.Equal(t, 3, smells[0].Line)
}

// Go build-constrained files: a //go:build line or a GOOS/GOARCH suffix.
func TestReview_GoBuildConstrained(t *testing.T) {
	assert.True(t, goBuildConstrained("a/hardlink_other.go", []byte("//go:build !unix && !windows\n\npackage a\n")))
	assert.True(t, goBuildConstrained("a/x_windows.go", []byte("package a\n")))
	assert.True(t, goBuildConstrained("a/x_linux_test.go", []byte("package a\n")))
	assert.True(t, goBuildConstrained("a/x_arm64.go", []byte("package a\n")))
	assert.False(t, goBuildConstrained("a/index.go", []byte("package a\n\n//go:build is not a constraint here\n")))
	assert.False(t, goBuildConstrained("a/windowsish.go", []byte("package a\n")))
}

// #396 G5: the STUB definition, case by case.
func TestStubBody(t *testing.T) {
	body := func(stmts []string, comments ...string) funcSpan {
		return funcSpan{HasBody: true, Stmts: stmts, Comments: comments}
	}
	cases := []struct {
		name string
		span funcSpan
		want string
	}{
		{"empty", body(nil), stubEmpty},
		{"python pass", body([]string{"pass"}), stubEmpty},
		{"python ellipsis", body([]string{"..."}), stubEmpty},
		{"docstring only", body([]string{`"""Does it."""`, "pass"}), stubEmpty},
		{"TODO only", body(nil, "// TODO: write it"), stubTodo},
		{"FIXME block comment", body(nil, "/* FIXME: later */"), stubTodo},
		{"documented no-op", body(nil, "// Nothing to release: the pool owns it."), ""},
		{"go panic", body([]string{`panic("not implemented")`}), stubNotImpl},
		{"go panic unimplemented", body([]string{`panic("unimplemented: x")`}), stubNotImpl},
		{"python raise", body([]string{`raise NotImplementedError("later")`}), stubNotImpl},
		{"ruby raise", body([]string{`raise NotImplementedError`}), stubNotImpl},
		{"ts throw", body([]string{`throw new Error("Not implemented");`}), stubNotImpl},
		{"java throw", body([]string{`throw new UnsupportedOperationException();`}), stubNotImpl},
		{"rust todo", body([]string{`todo!()`}), stubNotImpl},
		{"return true", body([]string{"return true"}), ""},
		{"return false", body([]string{"return false"}), ""},
		{"return nil", body([]string{"return nil"}), ""},
		{"return 0", body([]string{"return 0"}), ""},
		{"return empty string", body([]string{`return ""`}), ""},
		{"one-liner", body([]string{"return n + 1"}), ""},
		{"ruby implicit return", body([]string{"@balance + amount"}), ""},
		{"ruby puts", body([]string{"puts msg"}), ""},
		{"other panic", body([]string{`panic("unreachable")`}), ""},
		{"TODO next to a statement", body([]string{"x()"}, "// TODO: more"), ""},
		{"declaration without body", funcSpan{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, stubBody(c.span))
		})
	}
}

// A function with callers is never a STUB, whatever its body; an empty
// constructor is not one either.
func TestDetectStub_CallersAndConstructors(t *testing.T) {
	c := FunctionEdgeInfo{Name: "Flush", File: "a.go", Line: 3, Kind: "method", InEdges: 2}
	_, ok := detectStub(c, funcSpan{HasBody: true, Comments: []string{"// TODO: x"}}, reach{})
	assert.False(t, ok, "has callers")
	c.InEdges = 0
	_, ok = detectStub(c, funcSpan{HasBody: true, Comments: []string{"// TODO: x"}}, reach{})
	assert.True(t, ok, "no callers, TODO-only body")
	ctor := FunctionEdgeInfo{Name: "constructor", File: "a.ts", Line: 3, Kind: "method"}
	_, ok = detectStub(ctor, funcSpan{HasBody: true, Constructor: true}, reach{})
	assert.False(t, ok, "empty constructor")
	_, ok = detectStub(ctor, funcSpan{HasBody: true, Constructor: true, Comments: []string{"// TODO: wire"}}, reach{})
	assert.True(t, ok, "constructor with a TODO-only body")
}

// #396 G6: what reaches a function nobody calls, rule by rule.
func TestReview_Reach(t *testing.T) {
	goFile := &reviewFile{lang: "go"}
	goMain := &reviewFile{lang: "go", goMain: true}
	goTagged := &reviewFile{lang: "go", constrained: true}
	java := &reviewFile{lang: "java"}
	ts := &reviewFile{lang: "typescript"}
	decls := declIndex{
		declKey("java", "close"): {{file: "Closer.java", class: "Closer"}},
	}
	cases := []struct {
		name string
		f    *reviewFile
		c    FunctionEdgeInfo
		s    funcSpan
		want string
	}{
		{"go init", goFile, FunctionEdgeInfo{Name: "init", File: "a/a.go"}, funcSpan{}, "program entry point"},
		{"go main in main", goMain, FunctionEdgeInfo{Name: "main", File: "main.go"}, funcSpan{}, "program entry point"},
		{"go main elsewhere", goFile, FunctionEdgeInfo{Name: "main", File: "a/a.go"}, funcSpan{}, ""},
		{"go test", goFile, FunctionEdgeInfo{Name: "TestThing", File: "a/a_test.go"}, funcSpan{}, "test function, run by go test"},
		{"go testing helper", goFile, FunctionEdgeInfo{Name: "testhelper", File: "a/a_test.go"}, funcSpan{}, ""},
		{"go interface", goFile, FunctionEdgeInfo{Name: "Error", File: "a/a.go", Implements: "error"}, funcSpan{Class: "e"}, "reached through error"},
		{"go build tag", goTagged, FunctionEdgeInfo{Name: "hardLinked", File: "a/hardlink_other.go"}, funcSpan{}, "in a build-constrained file, one platform's variant"},
		{"go exported", goFile, FunctionEdgeInfo{Name: "Export", File: "a/a.go"}, funcSpan{Exported: true}, "exported API of a library package"},
		{"go unexported", goFile, FunctionEdgeInfo{Name: "helper", File: "a/a.go"}, funcSpan{}, ""},
		{"java constructor", java, FunctionEdgeInfo{Name: "Pipe", File: "Pipe.java"}, funcSpan{Class: "Pipe", Constructor: true}, "constructor, called implicitly"},
		{"java @Test", java, FunctionEdgeInfo{Name: "checks", File: "src/A.java"}, funcSpan{Class: "A", Annotations: []string{"Test"}}, "test function, run by the test runner"},
		{"java implements", java, FunctionEdgeInfo{Name: "close", File: "Pipe.java"}, funcSpan{Name: "close", Class: "Pipe", ClassHasBases: true}, "implements an interface or abstract method"},
		{"java same name, no bases", java, FunctionEdgeInfo{Name: "close", File: "Door.java"}, funcSpan{Name: "close", Class: "Door"}, ""},
		{"java @Override", java, FunctionEdgeInfo{Name: "toString", File: "A.java"}, funcSpan{Class: "A", Annotations: []string{"Override"}}, "overrides a base-class method"},
		{"ts exported", ts, FunctionEdgeInfo{Name: "api", File: "a.ts"}, funcSpan{Exported: true}, "exported API of a library package"},
		{"ts main", ts, FunctionEdgeInfo{Name: "main", File: "a.ts"}, funcSpan{}, "program entry point"},
		{"ts dead", ts, FunctionEdgeInfo{Name: "unused", File: "a.ts"}, funcSpan{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.f.reach(c.c, c.s, decls).reason)
		})
	}
}
