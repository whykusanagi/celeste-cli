package checkpoints

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionDirIsSafe(t *testing.T) {
	root := filepath.Join("r")
	assert.Equal(t, filepath.Join(root, "20261002-abc_1"), SessionDir(root, "20261002-abc_1"))
	assert.Equal(t, filepath.Join(root, "a_b_c"), SessionDir(root, "a/b\\c"))
	assert.Equal(t, filepath.Join(root, "_.."), SessionDir(root, ".."))
	assert.Equal(t, filepath.Join(root, "_"), SessionDir(root, ""))
}

func TestNewSnapshotManagerUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sm := NewSnapshotManager("chat-1")
	assert.Equal(t, filepath.Join(home, ".celeste", "checkpoints", "chat-1"), sm.Dir())
}

// mkSession creates a session directory last changed at changed: with an
// index.json (2.0) or, legacy, with one backup and no index (1.x).
func mkSession(t *testing.T, root, name string, changed time.Time, legacy bool) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	if legacy {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "0000_a.txt_v1"), []byte("x"), 0o600))
	} else {
		idx := filepath.Join(dir, "index.json")
		require.NoError(t, os.WriteFile(idx, []byte("[]\n"), 0o600))
		require.NoError(t, os.Chtimes(idx, changed, changed))
	}
	require.NoError(t, os.Chtimes(dir, changed, changed))
}

func sessionsIn(t *testing.T, root string) []string {
	t.Helper()
	des, err := os.ReadDir(root)
	require.NoError(t, err)
	var out []string
	for _, d := range des {
		out = append(out, d.Name())
	}
	return out
}

var pruneNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// Ruling 8: the 20 most recent survive, and so does anything under 30
// days; the union is kept.
func TestPruneKeepsTheLast20OrAnythingUnder30Days(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 25; i++ { // s00 newest; two days apart
		mkSession(t, root, fmt.Sprintf("s%02d", i), pruneNow.Add(-time.Duration(i)*48*time.Hour), false)
	}
	require.NoError(t, Prune(root, "", pruneNow))
	got := sessionsIn(t, root)
	assert.Len(t, got, 20)
	assert.Contains(t, got, "s19")
	assert.NotContains(t, got, "s20")

	// Recent sessions beyond 20 all stay: under 30 days wins.
	recent := t.TempDir()
	for i := 0; i < 25; i++ {
		mkSession(t, recent, fmt.Sprintf("r%02d", i), pruneNow.Add(-time.Duration(i)*time.Hour), false)
	}
	require.NoError(t, Prune(recent, "", pruneNow))
	assert.Len(t, sessionsIn(t, recent), 25)
}

func TestPruneNeverRemovesTheCurrentSession(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 21; i++ {
		mkSession(t, root, fmt.Sprintf("s%02d", i), pruneNow.Add(-time.Duration(40+i)*24*time.Hour), false)
	}
	require.NoError(t, Prune(root, "s20", pruneNow)) // s20 is the oldest
	got := sessionsIn(t, root)
	assert.Contains(t, got, "s20")
	assert.NotContains(t, got, "s19")
}

// Review Focus 5: 1.x checkpoint directories (no index) age by the
// directory's time.
func TestPruneUsesDirectoryTimeWithoutIndex(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		mkSession(t, root, fmt.Sprintf("s%02d", i), pruneNow.Add(-time.Duration(i)*time.Hour), false)
	}
	mkSession(t, root, "cli-123", pruneNow.Add(-60*24*time.Hour), true)
	mkSession(t, root, "chat-456", pruneNow.Add(-5*24*time.Hour), true)
	require.NoError(t, Prune(root, "", pruneNow))
	got := sessionsIn(t, root)
	assert.NotContains(t, got, "cli-123")
	assert.Contains(t, got, "chat-456")
}

func TestPruneWithoutRoot(t *testing.T) {
	assert.NoError(t, Prune(filepath.Join(t.TempDir(), "missing"), "", pruneNow))
}

// "Pruned at startup": the first store a process opens prunes, later ones
// do not.
func TestNewSnapshotManagerPrunesOnceAtStartup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	startupPrune = new(sync.Once)
	t.Cleanup(func() { startupPrune = new(sync.Once) })
	root := Root()
	old := time.Now().Add(-60 * 24 * time.Hour)
	for i := 0; i < 21; i++ {
		mkSession(t, root, fmt.Sprintf("s%02d", i), old.Add(-time.Duration(i)*time.Hour), false)
	}

	NewSnapshotManager("current")
	assert.Len(t, sessionsIn(t, root), 20, "the oldest of 21 old sessions is pruned")

	mkSession(t, root, "s99", old.Add(-1000*time.Hour), false)
	NewSnapshotManager("current")
	assert.Contains(t, sessionsIn(t, root), "s99", "pruning runs once per process")
}

// checkpointIn records a change to path in session sid under root, with the
// file's content before and after.
func checkpointIn(t *testing.T, root, sid, path, before, after string) Entry {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(before), 0o644))
	sm := newSnapshotManagerWithBase(SessionDir(root, sid))
	c, err := sm.Checkpoint(path, "call_"+sid)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(after), 0o644))
	return c.Entry()
}

func TestRevertFileDefaultsToTheLatestSession(t *testing.T) {
	root, work := t.TempDir(), t.TempDir()
	f := filepath.Join(work, "a.txt")
	checkpointIn(t, root, "older", f, "v0", "v1")
	time.Sleep(50 * time.Millisecond) // entry times must differ (coarse clocks)
	checkpointIn(t, root, "newer", f, "v1", "v2")

	sid, err := LatestSessionFor(root, f)
	require.NoError(t, err)
	assert.Equal(t, "newer", sid)

	sid, e, err := RevertFile(root, f, "")
	require.NoError(t, err)
	assert.Equal(t, "newer", sid)
	assert.Equal(t, "call_newer", e.MessageID)
	got, _ := os.ReadFile(f)
	assert.Equal(t, "v1", string(got))

	sid, _, err = RevertFile(root, f, "older")
	require.NoError(t, err)
	assert.Equal(t, "older", sid)
	got, _ = os.ReadFile(f)
	assert.Equal(t, "v0", string(got))

	_, _, err = RevertFile(root, f, "")
	assert.ErrorContains(t, err, "no checkpoint")
}

func TestRevertFileUnknownSession(t *testing.T) {
	root, work := t.TempDir(), t.TempDir()
	_, _, err := RevertFile(root, filepath.Join(work, "a.txt"), "nope")
	assert.ErrorContains(t, err, "no checkpoint")
}

// Review Focus 3: a path through a symlinked directory finds the entry the
// tool recorded under the other spelling.
func TestRevertFileMatchesThroughSymlinks(t *testing.T) {
	root, real := t.TempDir(), t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	checkpointIn(t, root, "s1", filepath.Join(link, "a.txt"), "before", "after")
	_, _, err := RevertFile(root, filepath.Join(real, "a.txt"), "")
	require.NoError(t, err)
	got, _ := os.ReadFile(filepath.Join(real, "a.txt"))
	assert.Equal(t, "before", string(got))
}

// A session another process is changing right now (a fresh index.lock) is
// never pruned, however old its last finished change.
func TestPruneSkipsALockedSession(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 21; i++ {
		mkSession(t, root, fmt.Sprintf("s%02d", i), pruneNow.Add(-time.Duration(40+i)*24*time.Hour), false)
	}
	lock := filepath.Join(root, "s20", "index.lock")
	require.NoError(t, os.WriteFile(lock, []byte("token"), 0o600))
	held := pruneNow.Add(-time.Minute)
	require.NoError(t, os.Chtimes(lock, held, held))
	require.NoError(t, os.Chtimes(filepath.Join(root, "s20"), held, held))
	require.NoError(t, os.Chtimes(filepath.Join(root, "s20", "index.json"), pruneNow.Add(-60*24*time.Hour), pruneNow.Add(-60*24*time.Hour)))
	require.NoError(t, Prune(root, "", pruneNow))
	assert.Contains(t, sessionsIn(t, root), "s20")
}
