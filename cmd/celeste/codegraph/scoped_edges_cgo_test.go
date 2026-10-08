//go:build cgo

package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scopedEdgesSrc has same-named methods in two classes. Each run calls a
// different module function, and each go calls its own class's helper.
const scopedEdgesSrc = `def alpha():
    pass

def beta():
    pass

class A:
    def run(self):
        alpha()
    def helper(self):
        pass
    def go(self):
        self.helper()

class B:
    def run(self):
        beta()
    def helper(self):
        pass
    def go(self):
        self.helper()
`

// scopedEdgeKeys names each edge of file by "Scope.name -> Scope.name".
func scopedEdgeKeys(t *testing.T, idx *Indexer, file string) map[string]bool {
	t.Helper()
	rows, err := idx.store.db.Query(`
		SELECT COALESCE(s.scope, ''), s.name, COALESCE(d.scope, ''), d.name
		FROM edges e JOIN symbols s ON s.id = e.source_id JOIN symbols d ON d.id = e.target_id
		WHERE s.file = ?`, file)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var ss, sn, ds, dn string
		require.NoError(t, rows.Scan(&ss, &sn, &ds, &dn))
		out[ss+"."+sn+" -> "+ds+"."+dn] = true
	}
	require.NoError(t, rows.Err())
	return out
}

func assertScopedEdges(t *testing.T, got map[string]bool) {
	t.Helper()
	for _, want := range []string{
		"A.run -> .alpha", "B.run -> .beta",
		"A.go -> A.helper", "B.go -> B.helper",
	} {
		assert.True(t, got[want], "missing edge %s (got %v)", want, got)
	}
	for _, bad := range []string{"A.run -> .beta", "A.go -> B.helper", "B.go -> A.helper"} {
		assert.False(t, got[bad], "edge attributed to the wrong class: %s", bad)
	}
}

// Aikido review of #414: methods of one name in different classes are
// separate rows, so their call edges must start (and, for self calls, end)
// at the method of the right class, not at the first one of that name.
func TestBuild_ScopedMethodEdgesKeepTheirClass(t *testing.T) {
	idx, _ := buildFixture(t, map[string]string{"m.py": scopedEdgesSrc})
	assertScopedEdges(t, scopedEdgeKeys(t, idx, "m.py"))
}

func TestUpdate_ScopedMethodEdgesKeepTheirClass(t *testing.T) {
	idx, ws := buildFixture(t, map[string]string{"m.py": "x = 1\n"})
	require.NoError(t, os.WriteFile(filepath.Join(ws, "m.py"), []byte(scopedEdgesSrc), 0o644))
	require.NoError(t, idx.Update())
	assertScopedEdges(t, scopedEdgeKeys(t, idx, "m.py"))
}

// unscoped is edges without their SourceScope, for tests that check only
// which names an edge joins.
func unscoped(edges []RawEdge) []RawEdge {
	out := make([]RawEdge, len(edges))
	for i, e := range edges {
		e.SourceScope = ""
		out[i] = e
	}
	return out
}
