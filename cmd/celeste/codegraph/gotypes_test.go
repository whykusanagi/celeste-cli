package codegraph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goFixture is a small module exercising every #375 case. It has no
// third-party imports, so the type checker only needs the standard library
// and the test stays hermetic (go list runs with GOPROXY=off).
var goFixture = map[string]string{
	"go.mod": "module example.com/fx\n\ngo 1.22\n",

	// (1) bare-name collapse: same-named functions and methods in two packages
	// and two receivers in one file.
	"a/a.go": `package a

func update() {}

func A() { update() }

type T struct{}

func (T) Update() {}

func (*T) admit() {}

type U struct{}

func (U) Update() {}

func UseU(u U) { u.Update() }

func Admit(t *T) { t.admit() }
`,
	"b/b.go": `package b

import "example.com/fx/a"

func update() {}

func B() { update() }

func UseT(t a.T) { t.Update() }
`,

	// (2) struct field, selector chain, method value, function value,
	// parameters, maps/slices of funcs, factories.
	"calls/calls.go": `package calls

type C struct{}

func (C) Do() {}

type Inner struct{ c C }
type Outer struct{ in Inner }

func Chain(o Outer) { o.in.c.Do() }

func target1() {}

type S struct{ h func() }

func NewS() S { return S{h: target1} }

func (s S) Run() { s.h() }

func target2() {}

func FuncValue() {
	g := target2
	g()
}

func MethodValue(c C) {
	f := c.Do
	f()
}

func target3() {}

func apply(fn func()) { fn() }

func Passes() { apply(target3) }

func handleA() {}
func handleB() {}

var handlers = map[string]func(){"a": handleA}
var list = []func(){handleB}

func Dispatch(k string) { handlers[k]() }

func RunAll() {
	for _, h := range list {
		h()
	}
}

var registry = map[string]func(){}

func register(name string, fn func()) { registry[name] = fn }

func handleC() {}

func init() { register("c", handleC) }

func Fire(n string) { registry[n]() }

func target4() {}

func factory() func() { return target4 }

func UseFactory() { factory()() }

type Celsius float64

func Conv(x float64) Celsius { return Celsius(x) }
`,

	// (3) interfaces: module interface dispatch plus external interfaces.
	"iface/iface.go": `package iface

import "fmt"

type Doer interface{ Do() }

type Impl struct{}

func (Impl) Do() {}

type PtrImpl struct{}

func (*PtrImpl) Do() {}

type NotImpl struct{}

func (NotImpl) Do(x int) {}

func CallIface(d Doer) { d.Do() }

type myErr struct{}

func (myErr) Error() string { return "x" }

var _ = fmt.Sprint

type Formatted struct{}

func (Formatted) Format(f fmt.State, verb rune) {
}

func NeverCalled() {
}
`,

	// Fallback: a package that does not type-check, and a file excluded by
	// build constraints.
	"broken/broken.go": `package broken

func helperX() {}

func Broken() {
	undefinedThing()
	helperX()
}
`,
	"cgo/c.go": `package cgo

import "C"

func UsesC() { helperC() }

func helperC() {}
`,
	"tagged/ignored.go": `//go:build ignore

package tagged

func Lone() { other() }

func other() {}
`,
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	ws := t.TempDir()
	for name, body := range files {
		p := filepath.Join(ws, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	return ws
}

func buildFixture(t *testing.T, files map[string]string) (*Indexer, string) {
	t.Helper()
	ws := writeFixture(t, files)
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	return idx, ws
}

// edgeKeys returns every edge as "source -kind-> target", naming each end by
// its qualified name when it has one and by its bare name otherwise.
func edgeKeys(t *testing.T, idx *Indexer) map[string]bool {
	t.Helper()
	rows, err := idx.store.db.Query(`
		SELECT COALESCE(NULLIF(s.qual_name, ''), s.name), e.kind, COALESCE(NULLIF(d.qual_name, ''), d.name)
		FROM edges e JOIN symbols s ON s.id = e.source_id JOIN symbols d ON d.id = e.target_id`)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var src, kind, dst string
		require.NoError(t, rows.Scan(&src, &kind, &dst))
		out[src+" -"+kind+"-> "+dst] = true
	}
	return out
}

func requireEdges(t *testing.T, got map[string]bool, want ...string) {
	t.Helper()
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing edge %q; have:\n%s", w, dumpEdges(got))
		}
	}
}

func forbidEdges(t *testing.T, got map[string]bool, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if got[b] {
			t.Errorf("unexpected edge %q", b)
		}
	}
}

func dumpEdges(m map[string]bool) string {
	var s []string
	for k := range m {
		s = append(s, "  "+k)
	}
	sort.Strings(s)
	return strings.Join(s, "\n")
}

const fx = "example.com/fx/"

// requireGoToolchain skips a test that needs the standard library's source:
// the Docker suite runs the compiled tests where no Go toolchain is installed,
// and there the indexer correctly falls back to the approximate pass.
func requireGoToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("needs the go toolchain to type-check the standard library")
	}
}

func TestGoTypes_BareNameCollapse(t *testing.T) {
	idx, _ := buildFixture(t, goFixture)
	got := edgeKeys(t, idx)
	cases := []struct{ name, edge string }{
		{"package func", fx + "a.A -calls-> " + fx + "a.update"},
		{"other package func", fx + "b.B -calls-> " + fx + "b.update"},
		{"method by receiver type", fx + "b.UseT -calls-> (" + fx + "a.T).Update"},
		{"same-file other receiver", fx + "a.UseU -calls-> (" + fx + "a.U).Update"},
		{"pointer receiver", fx + "a.Admit -calls-> (*" + fx + "a.T).admit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { requireEdges(t, got, c.edge) })
	}
	forbidEdges(t, got,
		fx+"a.A -calls-> "+fx+"b.update",
		fx+"b.B -calls-> "+fx+"a.update",
		fx+"b.UseT -calls-> ("+fx+"a.U).Update",
		fx+"a.UseU -calls-> ("+fx+"a.T).Update",
	)
}

func TestGoTypes_SameNameMethodsInOneFileAreDistinctSymbols(t *testing.T) {
	idx, _ := buildFixture(t, goFixture)
	syms, err := idx.store.GetSymbolsByFile(filepath.FromSlash("a/a.go"))
	require.NoError(t, err)
	var updates []string
	for _, s := range syms {
		if s.Name == "Update" {
			updates = append(updates, s.QualName)
		}
	}
	sort.Strings(updates)
	assert.Equal(t, []string{"(" + fx + "a.T).Update", "(" + fx + "a.U).Update"}, updates)
}

func TestGoTypes_IndirectCalls(t *testing.T) {
	idx, _ := buildFixture(t, goFixture)
	got := edgeKeys(t, idx)
	p := fx + "calls."
	cases := []struct{ name, edge string }{
		{"selector chain", p + "Chain -calls-> (" + p + "C).Do"},
		{"struct field", "(" + p + "S).Run -calls-> " + p + "target1"},
		{"function value", p + "FuncValue -calls-> " + p + "target2"},
		{"method value", p + "MethodValue -calls-> (" + p + "C).Do"},
		{"func parameter", p + "apply -calls-> " + p + "target3"},
		{"map of funcs", p + "Dispatch -calls-> " + p + "handleA"},
		{"slice of funcs", p + "RunAll -calls-> " + p + "handleB"},
		{"registry via param", p + "Fire -calls-> " + p + "handleC"},
		{"factory call", p + "UseFactory -calls-> " + p + "factory"},
		{"factory result", p + "UseFactory -calls-> " + p + "target4"},
		{"value taken in literal", p + "NewS -references-> " + p + "target1"},
		{"value in package var", p + "handlers -references-> " + p + "handleA"},
		{"value passed as arg", p + "init#calls/calls.go -references-> " + p + "handleC"},
		{"type conversion", p + "Conv -references-> " + p + "Celsius"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { requireEdges(t, got, c.edge) })
	}
}

func TestGoTypes_InterfaceImplementations(t *testing.T) {
	requireGoToolchain(t)
	idx, _ := buildFixture(t, goFixture)
	got := edgeKeys(t, idx)
	p := fx + "iface."
	requireEdges(t, got,
		p+"CallIface -calls-> ("+p+"Doer).Do",
		"("+p+"Doer).Do -implements-> ("+p+"Impl).Do",
		"("+p+"Doer).Do -implements-> (*"+p+"PtrImpl).Do",
	)
	forbidEdges(t, got, "("+p+"Doer).Do -implements-> ("+p+"NotImpl).Do")

	syms, err := idx.store.GetSymbolsByFile(filepath.FromSlash("iface/iface.go"))
	require.NoError(t, err)
	impl := map[string]string{}
	for _, s := range syms {
		impl[s.QualName] = s.Implements
	}
	assert.Contains(t, impl["("+p+"Formatted).Format"], "fmt.Formatter")
	assert.Contains(t, impl["("+p+"myErr).Error"], "error")
	assert.Contains(t, impl["("+p+"Impl).Do"], "iface.Doer")
	assert.Empty(t, impl["("+p+"NotImpl).Do"])
	_, ok := impl["("+p+"Doer).Do"]
	assert.True(t, ok, "interface method should be its own symbol")
}

func TestGoTypes_InterfaceMethodsAreNotReportedDead(t *testing.T) {
	requireGoToolchain(t)
	idx, _ := buildFixture(t, goFixture)
	smells, err := idx.FindCodeSmells([]CodeSmellKind{SmellStub}, 100, true)
	require.NoError(t, err)
	reasons := map[string]string{}
	for _, s := range smells {
		if filepath.ToSlash(s.File) == "iface/iface.go" {
			reasons[s.Name] = s.Reason
		}
	}
	// Control: a genuinely uncalled empty function is still reported dead.
	assert.Contains(t, reasons["NeverCalled"], "likely dead code")
	// fmt.Formatter's Format is called by fmt, never by the module.
	require.Contains(t, reasons, "Format", "empty Format body is still a stub")
	assert.NotContains(t, reasons["Format"], "likely dead code")
	assert.Contains(t, reasons["Format"], "fmt.Formatter")
}

func TestGoTypes_FallbackIsRecordedAsApproximate(t *testing.T) {
	idx, _ := buildFixture(t, goFixture)
	stored, err := idx.store.FileResolutions()
	require.NoError(t, err)
	res := map[string]string{}
	for k, v := range stored {
		res[filepath.ToSlash(k)] = v
	}
	assert.Equal(t, GoResolutionTyped, res["a/a.go"])
	assert.Equal(t, GoResolutionTyped, res["calls/calls.go"])
	assert.Equal(t, GoResolutionApproximate, res["broken/broken.go"])
	assert.Equal(t, GoResolutionApproximate, res["tagged/ignored.go"])
	// cgo is analysed as disabled (release binaries are CGO_ENABLED=0),
	// independent of the host.
	assert.Equal(t, GoResolutionApproximate, res["cgo/c.go"])

	got := edgeKeys(t, idx)
	// Typed even though the package has an error elsewhere.
	requireEdges(t, got, fx+"broken.Broken -calls-> "+fx+"broken.helperX")
	// Heuristic (bare names) for the build-tag-excluded file.
	requireEdges(t, got, "Lone -calls-> other")
}

// Every init in a package has the same types.Func FullName; each one keeps
// its own calls instead of all landing on one file's init.
func TestGoTypes_InitFunctionsPerFile(t *testing.T) {
	idx, _ := buildFixture(t, map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.22\n",
		"p/x.go": "package p\n\nfunc init() { helperA() }\n\nfunc helperA() {}\n",
		"p/y.go": "package p\n\nfunc init() { helperB() }\n\nfunc helperB() {}\n",
	})
	rows, err := idx.store.db.Query(`
		SELECT s.file, d.name FROM edges e
		JOIN symbols s ON s.id = e.source_id JOIN symbols d ON d.id = e.target_id
		WHERE s.name = 'init' AND e.kind = 'calls'`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var file, tgt string
		require.NoError(t, rows.Scan(&file, &tgt))
		got[filepath.ToSlash(file)+" -> "+tgt] = true // the store keeps native paths
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, map[string]bool{"p/x.go -> helperA": true, "p/y.go -> helperB": true}, got)
}

func TestGoTypes_NoGoModDirectoriesStaySeparate(t *testing.T) {
	idx, _ := buildFixture(t, map[string]string{
		"one/main.go": "package main\n\nfunc main() { run() }\n\nfunc run() {}\n",
		"two/main.go": "package main\n\nfunc main() { run() }\n\nfunc run() {}\n",
	})
	got := edgeKeys(t, idx)
	requireEdges(t, got, "_/one.main -calls-> _/one.run", "_/two.main -calls-> _/two.run")
	forbidEdges(t, got, "_/one.main -calls-> _/two.run", "_/two.main -calls-> _/one.run")
}

func TestGoTypes_UpdateKeepsIncomingEdgesOfChangedFile(t *testing.T) {
	idx, ws := buildFixture(t, goFixture)
	// Change the callee's file; the caller in b/ is untouched.
	p := filepath.Join(ws, "a", "a.go")
	src, err := os.ReadFile(p)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, append(src, []byte("\nfunc Extra() { update() }\n")...), 0o644))
	require.NoError(t, idx.Update())

	got := edgeKeys(t, idx)
	requireEdges(t, got,
		fx+"b.UseT -calls-> ("+fx+"a.T).Update",
		fx+"a.Extra -calls-> "+fx+"a.update",
	)
}

func TestGoTypes_OldIndexVersionRebuilds(t *testing.T) {
	idx, _ := buildFixture(t, goFixture)
	_, err := idx.store.UpsertSymbol(Symbol{Name: "stale", Kind: SymbolFunction, Package: "x", File: "a/a.go"})
	require.NoError(t, err)
	require.NoError(t, idx.store.SetMeta(metaGraphVersion, []byte("1")))
	require.NoError(t, idx.Update())

	syms, err := idx.store.SearchSymbolsByName("stale")
	require.NoError(t, err)
	assert.Empty(t, syms, "a version bump must rebuild the index from scratch")
	v, err := idx.store.GetMeta(metaGraphVersion)
	require.NoError(t, err)
	assert.Equal(t, graphVersion, string(v))
}

// failAfterCtx is a context whose Err starts reporting cancellation after
// n calls, so a test can abort an index pass part-way through.
type failAfterCtx struct {
	context.Context
	n int
}

func (c *failAfterCtx) Err() error {
	if c.n <= 0 {
		return context.Canceled
	}
	c.n--
	return nil
}

// An upgrade from an older graph version only redoes the Go rows: other
// languages keep their symbols and edges even when the upgrade is cancelled,
// and the next Update finishes it.
func TestGoTypes_CancelledUpgradeKeepsOtherLanguages(t *testing.T) {
	files := map[string]string{}
	for k, v := range goFixture {
		files[k] = v
	}
	files["py/mod.py"] = "def caller():\n    callee()\n\ndef callee():\n    pass\n"
	idx, _ := buildFixture(t, files)
	before := edgeKeys(t, idx)
	requireEdges(t, before, "caller -calls-> callee", fx+"b.UseT -calls-> ("+fx+"a.T).Update")

	require.NoError(t, idx.store.SetMeta(metaGraphVersion, []byte("1")))
	err := idx.UpdateWithContext(&failAfterCtx{Context: context.Background(), n: 1})
	require.ErrorIs(t, err, context.Canceled)

	requireEdges(t, edgeKeys(t, idx), "caller -calls-> callee")
	syms, err := idx.store.SearchSymbolsByName("callee")
	require.NoError(t, err)
	assert.NotEmpty(t, syms, "a cancelled upgrade must not drop other languages' symbols")
	v, err := idx.store.GetMeta(metaGraphVersion)
	require.NoError(t, err)
	assert.Equal(t, "1", string(v), "the version is stamped only once the upgrade completes")

	require.NoError(t, idx.Update())
	got := edgeKeys(t, idx)
	requireEdges(t, got, "caller -calls-> callee", fx+"b.UseT -calls-> ("+fx+"a.T).Update")
	assert.Equal(t, len(before), len(got))
	v, err = idx.store.GetMeta(metaGraphVersion)
	require.NoError(t, err)
	assert.Equal(t, graphVersion, string(v))
}

// TestGoTypes_ThisRepository runs the Go pass over celeste's own cmd/celeste
// and checks call edges the old name-based heuristic got wrong or missed.
func TestGoTypes_ThisRepository(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks the whole cmd/celeste tree")
	}
	requireGoToolchain(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(root, "cmd", "celeste")); err != nil {
		t.Skip("needs the source tree (the Docker suite runs the compiled tests without it)")
	}
	var files []string
	require.NoError(t, filepath.WalkDir(filepath.Join(root, "cmd", "celeste"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") {
			rel, _ := filepath.Rel(root, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	}))

	res, err := analyzeGo(context.Background(), root, files)
	require.NoError(t, err)

	const mod = "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/"
	callers := map[string]map[string]bool{}
	for _, e := range res.edges {
		if callers[e.tgtQual] == nil {
			callers[e.tgtQual] = map[string]bool{}
		}
		callers[e.tgtQual][e.srcQual+" "+string(e.kind)] = true
	}
	has := func(target, src string, kind EdgeKind) {
		t.Helper()
		if !callers[target][src+" "+string(kind)] {
			var have []string
			for k := range callers[target] {
				have = append(have, k)
			}
			sort.Strings(have)
			t.Errorf("%s: missing %s caller %s; have %v", target, kind, src, have)
		}
	}

	// errorText is a tui helper; its callers are tui functions and methods.
	has(mod+"tui.errorText", "("+mod+"tui.AppModel).update", EdgeCalls)
	// stampNow is called from loop methods and the steering code.
	has(mod+"loop.stampNow", mod+"loop.TestStampNowStrictlyIncreases", EdgeCalls)
	// Bare-name collapse: acp's session.update and tui's AppModel.update
	// share a name but must not share callers.
	for src := range callers["("+mod+"tui.AppModel).update"] {
		assert.NotContains(t, src, mod+"acp.", "acp caller resolved onto tui.AppModel.update")
	}
	// A method only reached through an interface: every tools.Tool
	// implementation's Execute is the target of an implements edge.
	has("(*"+mod+"tools/builtin.CodeGraphTool).Execute", "("+mod+"tools.Tool).Execute", EdgeImplements)

	// Nearly all of cmd/celeste must type-check: only files excluded by
	// build constraints (windows, cgo, integration tags) fall back.
	approx := 0
	byFile := map[string]string{}
	for _, f := range res.files {
		byFile[filepath.ToSlash(f.rel)] = f.resolution
		if f.resolution == GoResolutionApproximate {
			approx++
		}
	}
	assert.Less(t, approx, len(res.files)/20, "too many files fell back to the AST heuristic")
	// llm's in-package tests import packages whose own tests import llm;
	// checking test files with the importable package made that a cycle.
	assert.Equal(t, GoResolutionTyped, byFile["cmd/celeste/llm/client.go"])
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		sym  Symbol
		want string
	}{
		{Symbol{Name: "Build", Kind: SymbolMethod, QualName: "(*example.com/x/codegraph.Indexer).Build"}, "(*codegraph.Indexer).Build"},
		{Symbol{Name: "Do", Kind: SymbolInterfaceMethod, QualName: "(example.com/fx/iface.Doer).Do"}, "(iface.Doer).Do"},
		{Symbol{Name: "Push", Kind: SymbolMethod, QualName: "(*m.List[T]).Push"}, "(*m.List[T]).Push"},
		{Symbol{Name: "run", Kind: SymbolFunction, QualName: "example.com/fx/a.run"}, "run"},
		{Symbol{Name: "Update", Kind: SymbolMethod}, "Update"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, DisplayName(c.sym))
	}
}

func TestGoTypes_SearchAndSummaryReportApproximate(t *testing.T) {
	requireGoToolchain(t)
	idx, _ := buildFixture(t, goFixture)
	results, err := idx.SemanticSearchWithOptions("Broken helperX undefinedThing", SemanticSearchOptions{TopK: 20})
	require.NoError(t, err)
	found := false
	for _, r := range results {
		if filepath.ToSlash(r.Symbol.File) == "broken/broken.go" {
			found = true
			assert.Contains(t, r.ConfidenceWarnings, WarnApproximateGraph)
		}
		if filepath.ToSlash(r.Symbol.File) == "a/a.go" {
			assert.NotContains(t, r.ConfidenceWarnings, WarnApproximateGraph)
		}
	}
	assert.True(t, found, "search should return a symbol from broken/broken.go")
	assert.Contains(t, idx.ProjectSummary(), "Go call graph: 4 files type-checked, 3 approximate")
}

func TestGoTypes_UpdateNoticesGoModChanges(t *testing.T) {
	idx, ws := buildFixture(t, goFixture)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module example.com/renamed\n\ngo 1.22\n"), 0o644))
	// No .go file changed: only go.mod did.
	require.NoError(t, idx.Update())

	got := edgeKeys(t, idx)
	requireEdges(t, got, "example.com/renamed/a.A -calls-> example.com/renamed/a.update")
	for k := range got {
		assert.NotContains(t, k, "example.com/fx/", "stale qualified name after a module rename")
	}
}

// go list never runs with the user's GOFLAGS=-mod=mod, which could rewrite
// the indexed workspace's go.mod/go.sum.
func TestGoListModFlag(t *testing.T) {
	root := t.TempDir()
	assert.Equal(t, "-mod=readonly", goListModFlag(root))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "vendor", "modules.txt"), nil, 0o644))
	assert.Equal(t, "-mod=vendor", goListModFlag(root))
}
