package checkpoints

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snap takes a checkpoint with no message ID (the tests that only need the
// backup).
func snap(sm *SnapshotManager, path string) error {
	_, err := sm.Checkpoint(path, "")
	return err
}

// backups lists the backup files in a session directory (not index.json).
func backups(t *testing.T, dir string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(dir, "*_v*"))
	require.NoError(t, err)
	return got
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func store(t *testing.T) (*SnapshotManager, string) {
	t.Helper()
	dir := t.TempDir()
	return newSnapshotManagerWithBase(filepath.Join(dir, "session")), dir
}

func TestCheckpointExistingFile(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "original")

	c, err := sm.Checkpoint(f, "call_1")
	require.NoError(t, err)
	e := c.Entry()
	assert.Equal(t, "call_1", e.MessageID)
	assert.Equal(t, f, e.Path)
	assert.Equal(t, 1, e.Version)
	require.NotEmpty(t, e.Backup)
	assert.Equal(t, "original", read(t, filepath.Join(sm.Dir(), e.Backup)))
	assert.Equal(t, []Entry{e}, sm.Entries())
}

// The index is the spec's {message_id, path, version, backup} (plus time).
func TestIndexIsTheSpecFormat(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "x")
	_, err := sm.Checkpoint(f, "call_1")
	require.NoError(t, err)

	var raw []map[string]any
	require.NoError(t, json.Unmarshal([]byte(read(t, filepath.Join(sm.Dir(), "index.json"))), &raw))
	require.Len(t, raw, 1)
	for _, k := range []string{"message_id", "path", "version", "backup", "time"} {
		assert.Contains(t, raw[0], k)
	}
	assert.Equal(t, "call_1", raw[0]["message_id"])
	assert.Equal(t, f, raw[0]["path"])
}

func TestCheckpointNewFileHasNoBackup(t *testing.T) {
	sm, dir := store(t)
	c, err := sm.Checkpoint(filepath.Join(dir, "new.txt"), "call_1")
	require.NoError(t, err)
	assert.Equal(t, "", c.Entry().Backup)
	assert.Len(t, sm.Entries(), 1)
}

func TestCheckpointRefusesADirectory(t *testing.T) {
	sm, dir := store(t)
	_, err := sm.Checkpoint(dir, "call_1")
	require.Error(t, err)
	assert.Empty(t, sm.Entries())
	_, statErr := os.Stat(filepath.Join(sm.Dir(), "index.json"))
	assert.True(t, os.IsNotExist(statErr), "nothing is written for a refused checkpoint")
}

func TestCheckpointVersionsPerPath(t *testing.T) {
	sm, dir := store(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "1")
	write(t, b, "1")
	for _, p := range []string{a, a, b, a} {
		require.NoError(t, snap(sm, p))
	}
	var versions []int
	for _, e := range sm.Entries() {
		versions = append(versions, e.Version)
	}
	assert.Equal(t, []int{1, 2, 1, 3}, versions)
}

func TestRollbackRestoresAndDropsTheEntry(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "original")
	c, err := sm.Checkpoint(f, "call_1")
	require.NoError(t, err)
	write(t, f, "half-writ")

	require.NoError(t, c.Rollback())
	assert.Equal(t, "original", read(t, f))
	assert.Empty(t, sm.Entries())
	assert.Empty(t, backups(t, sm.Dir()))
	assert.Equal(t, "[]\n", read(t, filepath.Join(sm.Dir(), "index.json")))
	require.NoError(t, c.Rollback(), "a second rollback is a no-op")
}

func TestRollbackOfANewFileRemovesIt(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "new.txt")
	c, err := sm.Checkpoint(f, "call_1")
	require.NoError(t, err)
	write(t, f, "partial")
	require.NoError(t, c.Rollback())
	_, statErr := os.Stat(f)
	assert.True(t, os.IsNotExist(statErr))
	assert.Empty(t, sm.Entries())
}

func TestRevertAndRevertLast(t *testing.T) {
	sm, dir := store(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a0")
	require.NoError(t, snap(sm, a))
	write(t, a, "a1")
	require.NoError(t, snap(sm, b)) // b is new
	write(t, b, "b1")

	e, err := sm.RevertLast()
	require.NoError(t, err)
	assert.Equal(t, b, e.Path)
	_, statErr := os.Stat(b)
	assert.True(t, os.IsNotExist(statErr), "undoing a creation deletes the file")

	e, err = sm.Revert(a)
	require.NoError(t, err)
	assert.Equal(t, 1, e.Version)
	assert.Equal(t, "a0", read(t, a))
	assert.Empty(t, sm.Entries())

	_, err = sm.Revert(a)
	assert.Error(t, err)
	_, err = sm.RevertLast()
	assert.Error(t, err)
}

// W4's /rewind: every change from the given message on is undone, newest
// first.
func TestRewindTo(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "v0")
	for i, id := range []string{"call_1", "call_2", "call_3"} {
		_, err := sm.Checkpoint(f, id)
		require.NoError(t, err)
		write(t, f, fmt.Sprintf("v%d", i+1))
	}
	undone, err := sm.RewindTo("call_2")
	require.NoError(t, err)
	require.Len(t, undone, 2)
	assert.Equal(t, "call_3", undone[0].MessageID)
	assert.Equal(t, "call_2", undone[1].MessageID)
	assert.Equal(t, "v1", read(t, f))
	require.Len(t, sm.Entries(), 1)

	_, err = sm.RewindTo("call_9")
	assert.Error(t, err)
}

// #200: the files-modified list.
func TestFilesAreSortedAndUnique(t *testing.T) {
	sm, dir := store(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "x")
	write(t, b, "x")
	for _, p := range []string{b, a, b} {
		require.NoError(t, snap(sm, p))
	}
	assert.Equal(t, []string{a, b}, sm.Files())
}

// Review Focus 4: a resumed session sees what an earlier process recorded.
func TestStoreReloadsIndex(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "before")
	_, err := sm.Checkpoint(f, "call_1")
	require.NoError(t, err)
	write(t, f, "after")

	again := newSnapshotManagerWithBase(sm.Dir())
	require.Equal(t, sm.Entries()[0].Path, again.Entries()[0].Path)
	e, err := again.RevertLast()
	require.NoError(t, err)
	assert.Equal(t, "call_1", e.MessageID)
	assert.Equal(t, "before", read(t, f))
}

// Review Focus 5: a damaged index never blocks writes.
func TestStoreTreatsCorruptIndexAsEmpty(t *testing.T) {
	_, dir := store(t)
	sdir := filepath.Join(dir, "session")
	require.NoError(t, os.MkdirAll(sdir, 0o700))
	write(t, filepath.Join(sdir, "index.json"), "{not json")
	sm := newSnapshotManagerWithBase(sdir)
	assert.Empty(t, sm.Entries())

	f := filepath.Join(dir, "a.txt")
	write(t, f, "x")
	require.NoError(t, snap(sm, f))
	var entries []Entry
	require.NoError(t, json.Unmarshal([]byte(read(t, filepath.Join(sdir, "index.json"))), &entries))
	assert.Len(t, entries, 1)
}

func TestCleanupRemovesTheSession(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "x")
	require.NoError(t, snap(sm, f))
	require.NoError(t, sm.Cleanup())
	_, err := os.Stat(sm.Dir())
	assert.True(t, os.IsNotExist(err))
	assert.Empty(t, sm.Entries())
}

// Ruling 11: past the cap the oldest entry and its backup go, in the same
// index rewrite.
func TestCapEvictsOldestEntryAndBackup(t *testing.T) {
	sm, dir := store(t)
	sm.maxCount = 3
	f := filepath.Join(dir, "a.txt")
	for i := 0; i < 5; i++ {
		write(t, f, fmt.Sprintf("content %d", i))
		require.NoError(t, snap(sm, f))
	}
	entries := sm.Entries()
	require.Len(t, entries, 3)
	assert.Equal(t, 3, entries[0].Version)
	assert.Equal(t, 5, entries[2].Version)
	assert.Len(t, backups(t, sm.Dir()), 3)
	assert.Len(t, newSnapshotManagerWithBase(sm.Dir()).Entries(), 3, "the eviction is on disk")

	write(t, f, "edited")
	_, err := sm.Revert(f)
	require.NoError(t, err)
	assert.Equal(t, "content 4", read(t, f))
}

func TestDefaultCapIs100(t *testing.T) {
	sm, _ := store(t)
	assert.Equal(t, 100, sm.maxCount)
}

func TestConcurrentCheckpointsPastTheCap(t *testing.T) {
	sm, dir := store(t)
	sm.maxCount = 10
	var wg sync.WaitGroup
	errs := make(chan error, 80)
	for g := 0; g < 8; g++ {
		p := filepath.Join(dir, fmt.Sprintf("g%d.txt", g))
		write(t, p, "x")
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				errs <- snap(sm, p)
			}
		}(p)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Len(t, sm.Entries(), 10)
	assert.Len(t, backups(t, sm.Dir()), 10)
	assert.Len(t, newSnapshotManagerWithBase(sm.Dir()).Entries(), 10)
}

func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	assert.True(t, samePath(f, filepath.Join(dir, ".", "a.txt")))
	assert.False(t, samePath(f, filepath.Join(dir, "b.txt")))
}

// A hand-edited index cannot make a restore read, or a cleanup delete, a
// file outside the session directory.
func TestBackupNamesOutsideTheSessionAreRefused(t *testing.T) {
	_, dir := store(t)
	sdir := filepath.Join(dir, "session")
	require.NoError(t, os.MkdirAll(sdir, 0o700))
	victim := filepath.Join(dir, "victim.txt")
	write(t, victim, "keep me")
	target := filepath.Join(dir, "a.txt")
	write(t, target, "now")
	idx := []Entry{{Path: target, Version: 1, Backup: filepath.Join("..", "victim.txt")}}
	data, err := json.Marshal(idx)
	require.NoError(t, err)
	write(t, filepath.Join(sdir, "index.json"), string(data))

	sm := newSnapshotManagerWithBase(sdir)
	_, err = sm.RevertLast()
	require.Error(t, err)
	assert.Equal(t, "now", read(t, target))
	assert.Equal(t, "keep me", read(t, victim))
}

// A leftover symlink under a backup's name is replaced, not written through.
func TestBackupNeverWritesThroughASymlink(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "secret")
	outside := filepath.Join(dir, "outside.txt")
	write(t, outside, "untouched")
	require.NoError(t, os.MkdirAll(sm.Dir(), 0o700))
	if err := os.Symlink(outside, filepath.Join(sm.Dir(), backupName(f, 1))); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	c, err := sm.Checkpoint(f, "")
	require.NoError(t, err)
	assert.Equal(t, "untouched", read(t, outside))
	assert.Equal(t, "secret", read(t, filepath.Join(sm.Dir(), c.Entry().Backup)))
}

// Another process on the same session (celeste revert, a second window):
// each change starts from the index on disk, so neither undoes the other's
// bookkeeping.
func TestStoreSeesAnotherProcessesChanges(t *testing.T) {
	sm, dir := store(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a0")
	require.NoError(t, snap(sm, a))
	write(t, a, "a1")

	other := newSnapshotManagerWithBase(sm.Dir())
	_, err := other.RevertLast()
	require.NoError(t, err)
	assert.Equal(t, "a0", read(t, a))

	write(t, b, "b0")
	require.NoError(t, snap(sm, b))
	assert.Equal(t, []string{b}, sm.Files(), "the entry the other process reverted stays gone")
	assert.Len(t, newSnapshotManagerWithBase(sm.Dir()).Entries(), 1)
}

// Two processes on one session (two stores on one directory, each with its
// own mutex): the session lock keeps every entry either records.
func TestTwoStoresOnOneSessionLoseNothing(t *testing.T) {
	sm, dir := store(t)
	other := newSnapshotManagerWithBase(sm.Dir())
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for s, m := range []*SnapshotManager{sm, other} {
		p := filepath.Join(dir, fmt.Sprintf("p%d.txt", s))
		write(t, p, "x")
		wg.Add(1)
		go func(m *SnapshotManager, p string) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				errs <- snap(m, p)
			}
		}(m, p)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Len(t, newSnapshotManagerWithBase(sm.Dir()).Entries(), 40)
	assert.Len(t, backups(t, sm.Dir()), 40)
	_, err := os.Stat(filepath.Join(sm.Dir(), "index.lock"))
	assert.True(t, os.IsNotExist(err), "the lock is released")
}

// A lock left by a process that died is taken over once it is stale.
func TestStaleSessionLockIsTakenOver(t *testing.T) {
	sm, dir := store(t)
	require.NoError(t, os.MkdirAll(sm.Dir(), 0o700))
	lock := filepath.Join(sm.Dir(), "index.lock")
	write(t, lock, "")
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(lock, old, old))
	f := filepath.Join(dir, "a.txt")
	write(t, f, "x")
	require.NoError(t, snap(sm, f))
	assert.Len(t, sm.Entries(), 1)
}

// A rollback after a write that changed nothing (it failed up front, as on
// a read-only file) drops the entry without rewriting the file.
func TestRollbackOfAnUnchangedFileOnlyDropsTheEntry(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "same")
	c, err := sm.Checkpoint(f, "call_1")
	require.NoError(t, err)
	before, err := os.Stat(f)
	require.NoError(t, err)
	require.NoError(t, c.Rollback())
	after, err := os.Stat(f)
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "the file was not replaced")
	assert.Empty(t, sm.Entries())
}

// Backups of two files with the same base name never share a name: a weak
// path hash once mapped Aa/f.go and BB/f.go to one backup, so reverting one
// wrote the other's content.
func TestBackupNamesOfCollidingPathsDiffer(t *testing.T) {
	sm, dir := store(t)
	a, b := filepath.Join(dir, "Aa", "f.go"), filepath.Join(dir, "BB", "f.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(a), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(b), 0o755))
	write(t, a, "aa original")
	write(t, b, "bb original")
	ca, err := sm.Checkpoint(a, "")
	require.NoError(t, err)
	cb, err := sm.Checkpoint(b, "")
	require.NoError(t, err)
	assert.NotEqual(t, ca.Entry().Backup, cb.Entry().Backup)
	write(t, a, "aa edited")
	write(t, b, "bb edited")

	_, err = sm.Revert(a)
	require.NoError(t, err)
	assert.Equal(t, "aa original", read(t, a))
	_, err = sm.Revert(b)
	require.NoError(t, err)
	assert.Equal(t, "bb original", read(t, b))
}

// A backup name another live entry already uses is refused, never
// overwritten (a hand-edited or damaged index).
func TestCheckpointRefusesABackupNameInUse(t *testing.T) {
	sm, dir := store(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a")
	write(t, b, "b")
	require.NoError(t, os.MkdirAll(sm.Dir(), 0o700))
	idx := []Entry{{Path: b, Version: 1, Backup: backupName(a, 1)}}
	data, err := json.Marshal(idx)
	require.NoError(t, err)
	write(t, filepath.Join(sm.Dir(), "index.json"), string(data))

	_, err = sm.Checkpoint(a, "")
	require.Error(t, err)
	assert.Len(t, sm.Entries(), 1)
}

// A long file name still gets a backup (the name is shortened), so the
// write tools can edit the file.
func TestCheckpointOfAVeryLongFileName(t *testing.T) {
	sm, dir := store(t)
	name := ""
	for len(name) < 230 {
		name += "é"
	}
	f := filepath.Join(dir, name+".txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Skipf("filesystem refuses a long name: %v", err)
	}
	c, err := sm.Checkpoint(f, "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(c.Entry().Backup), 100)
	assert.True(t, utf8.ValidString(c.Entry().Backup))
	assert.Equal(t, "x", read(t, filepath.Join(sm.Dir(), c.Entry().Backup)))
}

// A holder whose stale lock was taken over does not release the new
// holder's lock when it finally finishes.
func TestUnlockLeavesAnotherHoldersLock(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockSession(dir, true)
	require.NoError(t, err)
	lock := filepath.Join(dir, "index.lock")
	write(t, lock, "someone-else")
	unlock()
	assert.Equal(t, "someone-else", read(t, lock))
}

// Two waiters both saw the same stale lock: the second must not remove the
// lock the first took after removing the stale one.
func TestTakeOverOnlyRemovesTheStaleLockItSaw(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "index.lock")
	write(t, lock, "first-waiters-new-lock")
	assert.False(t, takeOverLock(lock, "stale-token"))
	assert.Equal(t, "first-waiters-new-lock", read(t, lock))

	write(t, lock, "stale-token")
	assert.True(t, takeOverLock(lock, "stale-token"))
	_, err := os.Stat(lock)
	assert.True(t, os.IsNotExist(err))
	left, _ := filepath.Glob(filepath.Join(dir, "index.lock*"))
	assert.Empty(t, left, "no claim file is left behind")
}

// Two spellings of one existing file match: letter case on a
// case-insensitive volume (macOS by default), or a hard link.
func TestSamePathByFileIdentity(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "Name.txt")
	write(t, f, "x")
	if _, err := os.Stat(filepath.Join(dir, "name.txt")); err == nil {
		assert.True(t, samePath(f, filepath.Join(dir, "name.txt")), "case-insensitive volume")
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Link(f, link); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	assert.True(t, samePath(f, link))
	assert.False(t, samePath(f, filepath.Join(dir, "other.txt")))
}
