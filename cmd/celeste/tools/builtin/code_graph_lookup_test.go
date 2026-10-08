package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// buildLookupIndex indexes a Go module shaped like #395: two update methods
// among many Test…Update… functions, commands.Execute among more Execute
// methods (most of them test fakes) than one answer shows in detail.
func buildLookupIndex(t *testing.T) *codegraph.Indexer {
	t.Helper()
	ws := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"tui/app.go": `package tui

type AppModel struct{}

func (m AppModel) Update() AppModel { return m.update() }

func (m AppModel) update() AppModel { return m }

func (m AppModel) rewind() {}

func (m AppModel) RewindTo() { m.rewind() }
`,
		"acp/session.go": `package acp

type session struct{}

func (s *session) update() {}

func (s *session) finish() { s.update() }
`,
		"commands/commands.go": `package commands

func Execute(name string) string { return handleHelp() }

func handleHelp() string { return "help" }
`,
		"main.go": `package main

import "example.com/m/commands"

func main() { _ = commands.Execute("help") }
`,
	}
	var tests strings.Builder
	tests.WriteString("package tui\n\nimport \"testing\"\n\n")
	var fakes strings.Builder
	fakes.WriteString("package tui\n\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&tests, "func TestUpdate%d(t *testing.T) { AppModel{}.Update() }\n\n", i)
		fmt.Fprintf(&fakes, "type fake%d struct{}\n\nfunc (fake%d) Execute() {}\n\n", i, i)
	}
	files["tui/app_test.go"] = tests.String()
	files["tui/fakes_test.go"] = fakes.String()
	for name, body := range files {
		p := filepath.Join(ws, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	idx, err := codegraph.NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	return idx
}

func runGraph(t *testing.T, tool *CodeGraphTool, symbol, direction string) string {
	t.Helper()
	res, err := tool.Execute(context.Background(), map[string]any{"symbol": symbol, "direction": direction}, nil)
	require.NoError(t, err)
	require.False(t, res.Error, res.Content)
	return res.Content
}

// "update" reaches both update methods and none of the tests (#395).
func TestCodeGraphTool_ExactNameBeatsSubstrings(t *testing.T) {
	tool := NewCodeGraphTool(buildLookupIndex(t))
	out := runGraph(t, tool, "update", "callers")
	assert.Contains(t, out, "## (tui.AppModel).update (method)")
	assert.Contains(t, out, "## (*acp.session).update (method)")
	assert.Contains(t, out, "<- (tui.AppModel).Update (calls)")
	assert.Contains(t, out, "<- (*acp.session).finish (calls)")
	assert.NotContains(t, out, "## TestUpdate")

	out = runGraph(t, tool, "rewind", "callers")
	assert.Contains(t, out, "## (tui.AppModel).rewind (method)")
	assert.Contains(t, out, "<- (tui.AppModel).RewindTo (calls)")
	assert.NotContains(t, out, "## (tui.AppModel).RewindTo")
}

// The qualified names the tool prints reach exactly one symbol.
func TestCodeGraphTool_QualifiedNames(t *testing.T) {
	tool := NewCodeGraphTool(buildLookupIndex(t))
	for _, q := range []string{"(tui.AppModel).update", "AppModel.update", "tui.AppModel.update"} {
		out := runGraph(t, tool, q, "callers")
		assert.Contains(t, out, "## (tui.AppModel).update (method)", q)
		assert.NotContains(t, out, "acp.session", q)
		assert.NotContains(t, out, "not found", q)
	}
	out := runGraph(t, tool, "(*acp.session).update", "callers")
	assert.Contains(t, out, "## (*acp.session).update (method)")
	assert.NotContains(t, out, "tui.AppModel")

	out = runGraph(t, tool, "commands.Execute", "both")
	assert.Contains(t, out, "## Execute (function) — "+filepath.FromSlash("commands/commands.go")+":3")
	assert.Contains(t, out, "<- main (calls)")
	assert.Contains(t, out, "-> handleHelp (calls)")
	assert.NotContains(t, out, "fake")
}

// More same-named symbols than one answer details: every one is listed with
// its qualified name and file:line, non-test first, so the caller can pick.
func TestCodeGraphTool_ManySameNamedListsAll(t *testing.T) {
	tool := NewCodeGraphTool(buildLookupIndex(t))
	out := runGraph(t, tool, "Execute", "callers")
	assert.Contains(t, out, "11 symbols are named 'Execute'")
	first := strings.Index(out, "commands.Execute (function) — "+filepath.FromSlash("commands/commands.go")+":3")
	require.GreaterOrEqual(t, first, 0, out)
	for i := 0; i < 10; i++ {
		at := strings.Index(out, fmt.Sprintf("(tui.fake%d).Execute (method)", i))
		require.GreaterOrEqual(t, at, 0, out)
		assert.Greater(t, at, first, "non-test symbols come first")
	}
	assert.NotContains(t, out, "Called by")

	// Each listed name is accepted back.
	out = runGraph(t, tool, "(tui.fake7).Execute", "callers")
	assert.Contains(t, out, "## (tui.fake7).Execute (method)")
	assert.NotContains(t, out, "fake6")
}

// A query that only matches partly lists the candidates instead of
// detailing whatever sorts first.
func TestCodeGraphTool_PartialMatchesAreCandidates(t *testing.T) {
	tool := NewCodeGraphTool(buildLookupIndex(t))
	out := runGraph(t, tool, "pdat", "callers")
	assert.Contains(t, out, "No symbol is named 'pdat'")
	assert.Contains(t, out, "(tui.AppModel).update (method)")
	assert.Less(t, strings.Index(out, "(tui.AppModel).update"), strings.Index(out, "TestUpdate0"))

	out = runGraph(t, tool, "nosuchsymbol", "callers")
	assert.Contains(t, out, "Symbol 'nosuchsymbol' not found in the code graph.")
}

// Keyword search puts exact names first and prints qualified names, which
// code_graph accepts.
func TestCodeSearchTool_KeywordRanksExactFirst(t *testing.T) {
	tool := NewCodeSearchTool(buildLookupIndex(t))
	res, err := tool.Execute(context.Background(), map[string]any{"query": "update", "mode": "keyword", "limit": 3}, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "1. (*acp.session).update (method)")
	assert.Contains(t, res.Content, "2. (tui.AppModel).update (method)")
	assert.Contains(t, res.Content, "3. (tui.AppModel).Update (method)")
	assert.NotContains(t, res.Content, "TestUpdate")
}

// Lookup and depth together (#395 with #399): each same-named symbol an
// exact name details, and the one symbol a qualified name picks, is walked
// to the requested depth.
func TestCodeGraphTool_LookupWalksDepth(t *testing.T) {
	tool := NewCodeGraphTool(buildLookupIndex(t))
	query := func(symbol string) string {
		t.Helper()
		res, err := tool.Execute(context.Background(), map[string]any{"symbol": symbol, "direction": "callers", "depth": 2}, nil)
		require.NoError(t, err)
		require.False(t, res.Error, res.Content)
		return res.Content
	}

	out := query("update")
	assert.Contains(t, out, "2 symbols are named 'update'")
	assert.Contains(t, out, "## (tui.AppModel).update (method)")
	assert.Contains(t, out, "<- (tui.AppModel).Update (calls)")
	assert.Contains(t, out, "[hop 2, via (tui.AppModel).Update]")
	assert.Contains(t, out, "## (*acp.session).update (method)")
	assert.Contains(t, out, "<- (*acp.session).finish (calls)")

	out = query("(tui.AppModel).update")
	assert.NotContains(t, out, "acp.session")
	assert.Contains(t, out, "<- TestUpdate0 (calls)")
	assert.Contains(t, out, "[hop 2, via (tui.AppModel).Update]")

	out = query("handleHelp")
	assert.Contains(t, out, "<- Execute (calls)")
	assert.Contains(t, out, "<- main (calls)")
	assert.Contains(t, out, "[hop 2, via Execute]")
}

// #406: qualified names are unique over every match, not just the ones a
// limit keeps. Two packages named util both define Helper; with limit 1 the
// printed name must still tell them apart.
func TestCodeSearchTool_KeywordQualifiesBeforeLimit(t *testing.T) {
	ws := t.TempDir()
	files := map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.22\n",
		"a/util/u.go": "package util\n\nfunc Helper() {}\n",
		"b/util/u.go": "package util\n\nfunc Helper() {}\n",
		"main.go":     "package main\n\nfunc main() {}\n",
	}
	for name, body := range files {
		p := filepath.Join(ws, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	idx, err := codegraph.NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())

	full, err := NewCodeSearchTool(idx).Execute(context.Background(), map[string]any{"query": "Helper", "mode": "keyword", "limit": 10}, nil)
	require.NoError(t, err)
	res, err := NewCodeSearchTool(idx).Execute(context.Background(), map[string]any{"query": "Helper", "mode": "keyword", "limit": 1}, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "Found 1 symbols")
	assert.NotContains(t, res.Content, "1. util.Helper ", res.Content)
	line := strings.SplitN(strings.SplitN(res.Content, "1. ", 2)[1], " ", 2)[0]
	assert.Contains(t, full.Content, "1. "+line+" ", "limited name must match the unlimited one")

	// The printed name picks exactly one symbol.
	g := runGraph(t, NewCodeGraphTool(idx), line, "callers")
	assert.NotContains(t, g, "b/util", g)
}
