package codegraph

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lookupStore holds the shapes of #395: two update methods buried under
// Test…Update… functions, one commands.Execute among many Execute methods
// (most of them test fakes), rewind next to RewindTo/RewindResult, and
// same-named functions in other languages.
func lookupStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	t.Cleanup(func() { _ = s.Close() })
	add := func(sym Symbol) {
		_, err := s.UpsertSymbol(sym)
		require.NoError(t, err)
	}
	// Tests first, so storage order cannot rank them ahead by accident.
	for i := 0; i < 6; i++ {
		add(Symbol{Name: fmt.Sprintf("TestUpdate%d", i), Kind: SymbolFunction, Package: "tui", File: "tui/app_test.go", Line: 10 + i,
			QualName: fmt.Sprintf("example.com/m/tui.TestUpdate%d", i)})
		add(Symbol{Name: "Execute", Kind: SymbolMethod, Package: "tools", File: fmt.Sprintf("tools/fake%d_test.go", i), Line: 5,
			QualName: fmt.Sprintf("(*example.com/m/tools.fake%d).Execute", i)})
	}
	add(Symbol{Name: "update", Kind: SymbolMethod, Package: "tui", File: "tui/app.go", Line: 555,
		QualName: "(example.com/m/tui.AppModel).update"})
	add(Symbol{Name: "update", Kind: SymbolMethod, Package: "acp", File: "acp/session.go", Line: 434,
		QualName: "(*example.com/m/acp.session).update"})
	add(Symbol{Name: "Execute", Kind: SymbolFunction, Package: "commands", File: "commands/commands.go", Line: 125,
		QualName: "example.com/m/commands.Execute"})
	add(Symbol{Name: "Execute", Kind: SymbolMethod, Package: "tools", File: "tools/base.go", Line: 27,
		QualName: "(*example.com/m/tools.BaseTool).Execute"})
	add(Symbol{Name: "RewindResult", Kind: SymbolStruct, Package: "tui", File: "tui/rewind.go", Line: 3,
		QualName: "example.com/m/tui.RewindResult"})
	add(Symbol{Name: "RewindTo", Kind: SymbolMethod, Package: "tui", File: "tui/rewind.go", Line: 9,
		QualName: "(*example.com/m/tui.Chat).RewindTo"})
	add(Symbol{Name: "rewind", Kind: SymbolMethod, Package: "tui", File: "tui/app.go", Line: 900,
		QualName: "(example.com/m/tui.AppModel).rewind"})
	// A generic receiver, as go/types prints it.
	add(Symbol{Name: "Get", Kind: SymbolMethod, Package: "cache", File: "cache/cache.go", Line: 7,
		QualName: "(*example.com/m/cache.Cache[K, V]).Get"})
	// An approximate Go file: no qualified name, only the signature.
	add(Symbol{Name: "view", Kind: SymbolMethod, Package: "broken", File: "broken/model.go", Line: 4,
		Signature: "func (m *Model) view() string"})
	add(Symbol{Name: "add", Kind: SymbolFunction, File: "pkg/core.py", Line: 1})
	add(Symbol{Name: "add", Kind: SymbolFunction, File: "src/math.ts", Line: 1})
	add(Symbol{Name: "a_b", Kind: SymbolFunction, File: "u.py", Line: 1})
	add(Symbol{Name: "axb", Kind: SymbolFunction, File: "u.py", Line: 2})
	return s
}

func lookupFiles(syms []Symbol) []string {
	out := make([]string, len(syms))
	for i, s := range syms {
		out[i] = fmt.Sprintf("%s:%d", s.File, s.Line)
	}
	return out
}

// An exact name wins over every symbol that merely contains it (#395).
func TestLookupSymbol_ExactNameFirst(t *testing.T) {
	s := lookupStore(t)
	res, err := s.LookupSymbol("update")
	require.NoError(t, err)
	assert.Equal(t, MatchExact, res.Match)
	assert.ElementsMatch(t, []string{"tui/app.go:555", "acp/session.go:434"}, lookupFiles(res.Symbols))

	res, err = s.LookupSymbol("rewind")
	require.NoError(t, err)
	assert.Equal(t, MatchExact, res.Match)
	assert.Equal(t, []string{"tui/app.go:900"}, lookupFiles(res.Symbols))
}

// Every symbol sharing the name comes back, non-test ones first.
func TestLookupSymbol_AllSameNamedNonTestFirst(t *testing.T) {
	s := lookupStore(t)
	res, err := s.LookupSymbol("Execute")
	require.NoError(t, err)
	assert.Equal(t, MatchExact, res.Match)
	require.Len(t, res.Symbols, 8)
	assert.ElementsMatch(t, []string{"commands/commands.go:125", "tools/base.go:27"}, lookupFiles(res.Symbols[:2]))
	for _, sym := range res.Symbols[2:] {
		assert.Contains(t, sym.File, "_test.go")
	}
}

// The qualified names the tools print are accepted back as input, and so
// are the usual ways of writing them.
func TestLookupSymbol_QualifiedNames(t *testing.T) {
	s := lookupStore(t)
	cases := map[string]string{
		"(tui.AppModel).update":                  "tui/app.go:555",
		"tui.AppModel.update":                    "tui/app.go:555",
		"AppModel.update":                        "tui/app.go:555",
		"(AppModel).update":                      "tui/app.go:555",
		"(example.com/m/tui.AppModel).update":    "tui/app.go:555",
		"(*acp.session).update":                  "acp/session.go:434",
		"acp.session.update":                     "acp/session.go:434",
		"session.update":                         "acp/session.go:434",
		"commands.Execute":                       "commands/commands.go:125",
		"example.com/m/commands.Execute":         "commands/commands.go:125",
		"(*tools.BaseTool).Execute":              "tools/base.go:27",
		"BaseTool.Execute":                       "tools/base.go:27",
		"(*cache.Cache).Get":                     "cache/cache.go:7",
		"Cache.Get":                              "cache/cache.go:7",
		"(*example.com/m/cache.Cache[K, V]).Get": "cache/cache.go:7",
		"(*Model).view":                          "broken/model.go:4",
		"broken.Model.view":                      "broken/model.go:4",
		"core.add":                               "pkg/core.py:1",
		"pkg.core.add":                           "pkg/core.py:1",
		"pkg/core.add":                           "pkg/core.py:1",
		"math.add":                               "src/math.ts:1",
		"(tui.appmodel).UPDATE":                  "tui/app.go:555",
	}
	for q, want := range cases {
		res, err := s.LookupSymbol(q)
		require.NoError(t, err, q)
		assert.Equal(t, MatchQualified, res.Match, q)
		assert.Equal(t, []string{want}, lookupFiles(res.Symbols), q)
	}
}

// QualifiedName prints a name LookupSymbol resolves to that symbol alone.
func TestLookupSymbol_QualifiedNameRoundTrips(t *testing.T) {
	s := lookupStore(t)
	for _, name := range []string{"update", "Execute", "rewind", "Get", "view", "add"} {
		res, err := s.LookupSymbol(name)
		require.NoError(t, err)
		for _, sym := range res.Symbols {
			q := QualifiedName(sym)
			back, err := s.LookupSymbol(q)
			require.NoError(t, err, q)
			assert.Equal(t, []string{fmt.Sprintf("%s:%d", sym.File, sym.Line)}, lookupFiles(back.Symbols), q)
		}
	}
	assert.Equal(t, "(tui.AppModel).update", QualifiedName(Symbol{Name: "update", Kind: SymbolMethod,
		QualName: "(example.com/m/tui.AppModel).update"}))
	assert.Equal(t, "commands.Execute", QualifiedName(Symbol{Name: "Execute", Kind: SymbolFunction,
		QualName: "example.com/m/commands.Execute"}))
}

// Case-insensitive exact names come before partial matches.
func TestLookupSymbol_CaseInsensitiveThenPartial(t *testing.T) {
	s := lookupStore(t)
	res, err := s.LookupSymbol("rewindto")
	require.NoError(t, err)
	assert.Equal(t, MatchExactFold, res.Match)
	assert.Equal(t, []string{"tui/rewind.go:9"}, lookupFiles(res.Symbols))

	res, err = s.LookupSymbol("ewin")
	require.NoError(t, err)
	assert.Equal(t, MatchPartial, res.Match)
	assert.Len(t, res.Symbols, 3)

	// Prefix matches rank before substring matches, non-test before test.
	res, err = s.LookupSymbol("Upd")
	require.NoError(t, err)
	assert.Equal(t, MatchPartial, res.Match)
	require.NotEmpty(t, res.Symbols)
	assert.Equal(t, "update", res.Symbols[0].Name)
	assert.Equal(t, "update", res.Symbols[1].Name)
}

// LIKE wildcards in the query are taken literally.
func TestLookupSymbol_EscapesWildcards(t *testing.T) {
	s := lookupStore(t)
	res, err := s.LookupSymbol("a_")
	require.NoError(t, err)
	assert.Equal(t, []string{"u.py:1"}, lookupFiles(res.Symbols))
	res, err = s.LookupSymbol("%")
	require.NoError(t, err)
	assert.Equal(t, MatchNone, res.Match)
	assert.Empty(t, res.Symbols)
}

// A qualified name with no qualified match falls back to the symbols with
// its last part as candidates, rather than "not found".
func TestLookupSymbol_UnknownQualifierListsCandidates(t *testing.T) {
	s := lookupStore(t)
	res, err := s.LookupSymbol("nosuch.update")
	require.NoError(t, err)
	assert.Equal(t, MatchPartial, res.Match)
	assert.Len(t, res.Symbols, 2)
}

// Keyword search ranks exact names first instead of alphabetical LIKE order.
func TestKeywordSearch_RanksExactFirst(t *testing.T) {
	s := lookupStore(t)
	syms, err := s.RankedSearch("update", 5)
	require.NoError(t, err)
	require.Len(t, syms, 5)
	assert.Equal(t, "update", syms[0].Name)
	assert.Equal(t, "update", syms[1].Name)
}

// Same-named symbols whose short qualified names collide (same package
// name, same file stem) are listed by a longer name that still selects one.
func TestQualifiedNames_DisambiguateCollisions(t *testing.T) {
	s := newTestStore(t)
	t.Cleanup(func() { _ = s.Close() })
	for _, sym := range []Symbol{
		{Name: "Helper", Kind: SymbolFunction, Package: "util", File: "a/util/u.go", Line: 3, QualName: "example.com/m/a/util.Helper"},
		{Name: "Helper", Kind: SymbolFunction, Package: "util", File: "b/util/u.go", Line: 3, QualName: "example.com/m/b/util.Helper"},
		{Name: "Helper", Kind: SymbolFunction, File: "x/core.py", Line: 1},
		{Name: "Helper", Kind: SymbolFunction, File: "y/core.py", Line: 1},
		{Name: "Helper", Kind: SymbolFunction, File: "z/other.py", Line: 1},
	} {
		_, err := s.UpsertSymbol(sym)
		require.NoError(t, err)
	}
	res, err := s.LookupSymbol("Helper")
	require.NoError(t, err)
	names := QualifiedNames(res.Symbols)
	assert.Equal(t, []string{
		"example.com/m/a/util.Helper", "example.com/m/b/util.Helper",
		"x/core.Helper", "y/core.Helper", "other.Helper",
	}, names)
	for i, q := range names {
		back, err := s.LookupSymbol(q)
		require.NoError(t, err, q)
		assert.Equal(t, lookupFiles(res.Symbols[i:i+1]), lookupFiles(back.Symbols), q)
	}
}

// On Windows the index stores paths with backslashes (filepath.Rel). The
// file-based scopes and printed names still use '/'.
func TestLookupSymbol_BackslashPaths(t *testing.T) {
	s := newTestStore(t)
	t.Cleanup(func() { _ = s.Close() })
	for _, sym := range []Symbol{
		{Name: "sub", Kind: SymbolFunction, File: `pkg\win.py`, Line: 1},
		{Name: "sub", Kind: SymbolFunction, File: `other\win.py`, Line: 1},
	} {
		_, err := s.UpsertSymbol(sym)
		require.NoError(t, err)
	}
	for _, q := range []string{"win.sub", "pkg/win.sub", "pkg.win.sub"} {
		res, err := s.LookupSymbol(q)
		require.NoError(t, err, q)
		assert.Equal(t, MatchQualified, res.Match, q)
	}
	res, err := s.LookupSymbol("pkg/win.sub")
	require.NoError(t, err)
	require.Len(t, res.Symbols, 1)
	assert.Equal(t, "win.sub", QualifiedName(res.Symbols[0]))

	all, err := s.LookupSymbol("sub")
	require.NoError(t, err)
	assert.Equal(t, []string{"other/win.sub", "pkg/win.sub"}, QualifiedNames(all.Symbols))
}

// Two same-named symbols in one file (a module function and a method) are
// told apart by their line: "pkg/core.add:9" selects one of them.
func TestQualifiedNames_SameFileCollisionsCarryLine(t *testing.T) {
	s := newTestStore(t)
	t.Cleanup(func() { _ = s.Close() })
	for _, sym := range []Symbol{
		{Name: "add", Kind: SymbolFunction, File: "pkg/core.py", Line: 1},
		{Name: "add", Kind: SymbolMethod, File: "pkg/core.py", Line: 9},
		{Name: "add", Kind: SymbolFunction, File: "lib/core.py", Line: 4},
		{Name: "add", Kind: SymbolFunction, File: "src/math.ts", Line: 2},
	} {
		_, err := s.UpsertSymbol(sym)
		require.NoError(t, err)
	}
	res, err := s.LookupSymbol("add")
	require.NoError(t, err)
	names := QualifiedNames(res.Symbols)
	assert.Equal(t, []string{"lib/core.add", "pkg/core.add:1", "pkg/core.add:9", "math.add"}, names)
	for i, q := range names {
		back, err := s.LookupSymbol(q)
		require.NoError(t, err, q)
		assert.NotEqual(t, MatchPartial, back.Match, q)
		assert.Equal(t, lookupFiles(res.Symbols[i:i+1]), lookupFiles(back.Symbols), q)
	}

	// A line that matches nothing lists the symbols of the name as candidates.
	miss, err := s.LookupSymbol("pkg/core.add:5")
	require.NoError(t, err)
	assert.Equal(t, MatchPartial, miss.Match)
	assert.Len(t, miss.Symbols, 2)
}
