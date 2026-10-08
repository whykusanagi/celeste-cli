//go:build cgo

package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Review of the class-method scopes: an upgrade that keeps a file it cannot
// parse stores its dropped edges again on the very symbols they joined. A
// same-named method of another class in the target file (Foo.add before
// Bar.add) does not take the edge that went to Bar.add.
func TestUpdate_UpgradeRestoresEdgeToTheSameScopedMethod(t *testing.T) {
	files := map[string]string{
		"a_run.py": "from b_lib import Bar\n\ndef run():\n    Bar().add(1)\n",
		"b_lib.py": "class Foo:\n    def add(self, a):\n        return a\n\nclass Bar:\n    def add(self, a):\n        return a\n",
	}
	idx, ws := buildFixture(t, files)
	db := idx.store.db
	var runID, barAdd int64
	require.NoError(t, db.QueryRow(`SELECT id FROM symbols WHERE name = 'run' AND file = 'a_run.py'`).Scan(&runID))
	require.NoError(t, db.QueryRow(`SELECT id FROM symbols WHERE name = 'add' AND scope = 'Bar'`).Scan(&barAdd))
	// The edge goes to Bar.add, whatever the name-only resolution chose.
	_, err := db.Exec(`DELETE FROM edges WHERE source_id = ?`, runID)
	require.NoError(t, err)
	require.NoError(t, idx.store.AddEdge(runID, barAdd, EdgeCalls))
	require.NoError(t, idx.store.SetMeta(metaGraphVersion, []byte("1")))

	testHookRecoveryParse = func(path string) {
		if filepath.Base(path) == "a_run.py" {
			require.NoError(t, os.Remove(filepath.Join(ws, path)))
		}
	}
	t.Cleanup(func() { testHookRecoveryParse = nil })
	require.NoError(t, idx.Update())
	testHookRecoveryParse = nil

	rows, err := db.Query(`
		SELECT COALESCE(d.scope, '') FROM edges e
		JOIN symbols s ON s.id = e.source_id JOIN symbols d ON d.id = e.target_id
		WHERE s.name = 'run' AND s.file = 'a_run.py' AND d.name = 'add'`)
	require.NoError(t, err)
	defer rows.Close()
	var scopes []string
	for rows.Next() {
		var sc string
		require.NoError(t, rows.Scan(&sc))
		scopes = append(scopes, sc)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"Bar"}, scopes, "the kept file's edge is stored again on Bar.add")
	requireFinished(t, idx)
}
