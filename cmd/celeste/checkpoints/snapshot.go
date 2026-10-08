// Package checkpoints keeps backups of the files celeste's write tools
// change, on disk per session, so a change can be undone later — also from
// another process (2.0 F4).
package checkpoints

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/pathutil"
)

// Entry is one checkpoint: Path as it was before a tool changed it.
type Entry struct {
	// MessageID is the ID of the tool call that made the change ("" when
	// unknown), so a rewind can map entries to messages.
	MessageID string `json:"message_id"`
	// Path is the absolute path of the changed file.
	Path string `json:"path"`
	// Version counts the session's changes to Path: 1, 2, …
	Version int `json:"version"`
	// Backup is the backup's file name in the session directory; "" when
	// the file did not exist, so undoing the change deletes it.
	Backup string `json:"backup"`
	// Time is when the checkpoint was taken (UTC).
	Time time.Time `json:"time"`
	// After is the file as the change left it, recorded when the tool's
	// write succeeded (Commit); nil until then, or when it could not be
	// read. Undoing the change compares the file with it (Changed).
	After *FileState `json:"after,omitempty"`
	// Root is the real directory undoing the change must stay inside (the
	// workspace, see CheckpointIn), and Rel the file's path inside it at
	// the time of the change. Undo reaches the file only through Root, so
	// a directory replaced by a symlink since cannot redirect it. Empty in
	// an index written before they existed.
	Root string `json:"root,omitempty"`
	Rel  string `json:"rel,omitempty"`
}

// FileState identifies a file's contents: its size and SHA-256.
type FileState struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// StateOf reads path's FileState.
func StateOf(path string) (FileState, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileState{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return FileState{}, err
	}
	return FileState{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// Changed reports whether e's file is no longer as e's change left it:
// something else (an editor, a formatter, a bash command, another write
// whose checkpoint failed) changed it since, so undoing e would overwrite
// that — or, for a file the change created, delete it. A file that is gone
// has nothing to lose (false). An entry whose After was never recorded
// counts as changed: celeste cannot tell. now is the file's state (zero
// when it is gone or unreadable).
func Changed(e Entry) (now FileState, changed bool, err error) {
	now, err = entryState(e)
	if errors.Is(err, os.ErrNotExist) {
		return FileState{}, false, nil
	}
	if err != nil {
		return FileState{}, true, err
	}
	return now, e.After == nil || *e.After != now, nil
}

// errNoCheckpoint: the session has no checkpoint of the file asked for.
var errNoCheckpoint = errors.New("no checkpoint")

const (
	indexFile         = "index.json"
	defaultMaxEntries = 100
	// evictedFile lists the call IDs (MessageID) of entries the cap
	// evicted, newest last, at most maxEvictedIDs of them: a rewind of
	// those calls can then say part of it was too old to restore.
	evictedFile   = "evicted.json"
	maxEvictedIDs = 1000
)

// SnapshotManager is one session's checkpoints: its backups and index.json
// in one directory. It is safe for concurrent use. Every change is written
// to the index before it takes effect in memory.
type SnapshotManager struct {
	dir      string
	entries  []Entry
	maxCount int // entries kept; past it the oldest entry and backup go
	mu       sync.Mutex
	// open holds this process's checkpoints whose write has not finished
	// (no Commit or Rollback yet), with when they were taken: undoing then
	// would race the write.
	open map[*Checkpoint]time.Time
}

// openStale is when an open checkpoint stops blocking undo: its tool was
// abandoned (a watchdog gave up on it) and may never finish.
const openStale = 2 * time.Minute

// errWriteInProgress: a write tool is between Checkpoint and Commit.
var errWriteInProgress = errors.New("a file write is in progress; try again when it finishes")

// writeInProgressLocked reports whether a checkpoint taken in this process
// is still open (not past openStale). Callers hold sm.mu.
func (sm *SnapshotManager) writeInProgressLocked() bool {
	for _, t := range sm.open {
		if time.Since(t) < openStale {
			return true
		}
	}
	return false
}

// startupPrune runs Prune once per process, on the first store opened:
// every mode opens one in loop.Setup, so that is startup.
var startupPrune = new(sync.Once)

// NewSnapshotManager opens the checkpoints of sessionID under Root(),
// with whatever an earlier process recorded for it (a resumed session).
// The first call in a process prunes old sessions first (never this one).
// Nothing is created on disk until the first checkpoint.
func NewSnapshotManager(sessionID string) *SnapshotManager {
	root := Root()
	startupPrune.Do(func() {
		if err := Prune(root, sessionID, time.Now()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: pruning old checkpoints: %v\n", err)
		}
	})
	return newSnapshotManagerWithBase(SessionDir(root, sessionID))
}

// newSnapshotManagerWithBase opens the session stored in dir. An index
// that cannot be read is treated as empty and overwritten by the next
// change (one warning).
func newSnapshotManagerWithBase(dir string) *SnapshotManager {
	entries, err := readIndex(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: checkpoint index %s is unreadable, starting empty: %v\n", filepath.Join(dir, indexFile), err)
		entries = nil
	}
	return &SnapshotManager{dir: dir, entries: entries, maxCount: defaultMaxEntries}
}

// Dir is the session's checkpoint directory.
func (sm *SnapshotManager) Dir() string { return sm.dir }

// Checkpoint is an entry its writer can still roll back.
type Checkpoint struct {
	sm    *SnapshotManager
	entry Entry
}

// Entry is the recorded entry.
func (c *Checkpoint) Entry() Entry { return c.entry }

// Checkpoint backs up path as it is now and records the entry in the
// index. Call it after the tool's input validated, immediately before the
// write, and Rollback the result if the write then fails (2.0 F4). A path
// that does not exist yet is recorded without a backup. A path that is not
// a regular file is refused and nothing is recorded. The backup is a full
// copy, read into memory, with no size limit: a session holds at most
// defaultMaxEntries of them, which bounds the count but not the bytes (a
// session that rewrites a large file keeps up to that many copies of it).
//
// Undoing the change stays inside path's own directory; the write tools use
// CheckpointIn to keep it inside the workspace.
func (sm *SnapshotManager) Checkpoint(path, messageID string) (*Checkpoint, error) {
	return sm.CheckpointIn("", path, messageID)
}

// CheckpointIn is Checkpoint for a path inside workspace: undoing the
// change (undo, rewind, celeste revert, Rollback) reaches the file only
// through workspace, so it cannot write or delete anything outside it, even
// after a directory on the way was replaced by a symlink. path's directory
// must exist and resolve inside workspace.
func (sm *SnapshotManager) CheckpointIn(workspace, path, messageID string) (*Checkpoint, error) {
	root, rel, ok := confine(workspace, path)
	if !ok && workspace != "" {
		return nil, fmt.Errorf("cannot snapshot %s: it is not inside the workspace", path)
	}
	if sm.dir == "" {
		// Checkpoints are off (no home directory): the write goes ahead
		// unrecorded, and rolling it back does nothing.
		return &Checkpoint{entry: Entry{MessageID: messageID, Path: path}}, nil
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	_, dirErr := os.Stat(sm.dir)
	unlock, err := lockSession(sm.dir, true)
	if err != nil {
		return nil, err
	}
	c, err := sm.checkpointLocked(path, messageID, root, rel)
	unlock()
	if err != nil && errors.Is(dirErr, os.ErrNotExist) {
		_ = os.Remove(sm.dir) // created for the lock only: leave nothing behind
	}
	if err == nil {
		if sm.open == nil {
			sm.open = map[*Checkpoint]time.Time{}
		}
		sm.open[c] = time.Now()
	}
	return c, err
}

// checkpointLocked is Checkpoint under sm.mu and the session lock.
func (sm *SnapshotManager) checkpointLocked(path, messageID, root, rel string) (*Checkpoint, error) {
	sm.reloadLocked()

	e := Entry{MessageID: messageID, Path: path, Version: sm.nextVersionLocked(path), Time: time.Now().UTC(), Root: root, Rel: rel}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// A new file: undoing the change deletes it.
	case err != nil:
		return nil, fmt.Errorf("cannot stat file for snapshot: %w", err)
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("cannot snapshot %s: not a regular file", path)
	default:
		if err := os.MkdirAll(sm.dir, 0o700); err != nil {
			return nil, fmt.Errorf("cannot create checkpoint directory: %w", err)
		}
		e.Backup = backupName(path, e.Version)
		for _, old := range sm.entries {
			if old.Backup == e.Backup {
				return nil, fmt.Errorf("cannot snapshot %s: backup %s is in use by another checkpoint", path, e.Backup)
			}
		}
		if err := backUp(path, filepath.Join(sm.dir, e.Backup), info); err != nil {
			return nil, err
		}
	}

	next := append(append([]Entry(nil), sm.entries...), e)
	var evicted []Entry
	for sm.maxCount > 0 && len(next) > sm.maxCount {
		evicted = append(evicted, next[0])
		next = next[1:]
	}
	if err := writeIndex(sm.dir, next); err != nil {
		sm.removeBackup(e)
		return nil, fmt.Errorf("cannot record checkpoint: %w", err)
	}
	sm.entries = next
	for _, old := range evicted {
		sm.removeBackup(old)
	}
	if err := sm.recordEvicted(evicted); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: recording evicted checkpoints: %v\n", err)
	}
	return &Checkpoint{sm: sm, entry: e}, nil
}

// Commit records the file as the write left it (Entry.After), once the
// tool's write succeeded, and closes the checkpoint. Every Checkpoint ends
// in Commit or Rollback. An entry already undone or evicted is left alone.
func (c *Checkpoint) Commit() error {
	sm := c.sm
	if sm == nil {
		return nil
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.open, c)
	st, err := entryState(c.entry)
	if err != nil {
		return fmt.Errorf("cannot record the state of %s: %w", c.entry.Path, err)
	}
	unlock, err := lockSession(sm.dir, false)
	if err != nil {
		return err
	}
	defer unlock()
	sm.reloadLocked()
	for i := len(sm.entries) - 1; i >= 0; i-- {
		if sameEntry(sm.entries[i], c.entry) {
			next := append([]Entry(nil), sm.entries...)
			next[i].After = &st
			if err := writeIndex(sm.dir, next); err != nil {
				return fmt.Errorf("cannot record checkpoint: %w", err)
			}
			sm.entries = next
			return nil
		}
	}
	return nil
}

// Rollback restores the file to its checkpointed state (deleting a file
// that did not exist) and removes the entry and its backup: for a write
// that failed after Checkpoint. After the entry was already undone,
// reverted or evicted it does nothing.
func (c *Checkpoint) Rollback() error {
	sm := c.sm
	if sm == nil {
		return nil
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.open, c)
	unlock, err := lockSession(sm.dir, false)
	if err != nil {
		return err
	}
	defer unlock()
	sm.reloadLocked()
	for i := len(sm.entries) - 1; i >= 0; i-- {
		if sameEntry(sm.entries[i], c.entry) {
			return sm.undoLocked(i)
		}
	}
	return nil
}

// Revert restores path from its newest entry and removes that entry.
func (sm *SnapshotManager) Revert(path string) (Entry, error) { return sm.RevertIf(path, nil) }

// RevertIf is Revert that first asks check (when not nil) about the entry
// it would undo, under the store's locks, and undoes nothing when check
// returns an error, which it returns.
func (sm *SnapshotManager) RevertIf(path string, check func(Entry) error) (Entry, error) {
	return sm.undoNewest(check, func(e Entry) bool { return pathutil.Same(e.Path, path) },
		fmt.Errorf("%w of %s in this session", errNoCheckpoint, path))
}

// RevertLast restores the file of the newest entry and removes it (/undo).
func (sm *SnapshotManager) RevertLast() (Entry, error) { return sm.RevertLastIf(nil) }

// RevertLastIf is RevertLast with check, as RevertIf.
func (sm *SnapshotManager) RevertLastIf(check func(Entry) error) (Entry, error) {
	return sm.undoNewest(check, func(Entry) bool { return true },
		errors.New("no file changes to undo in this session"))
}

// undoNewest undoes the newest entry match accepts, after check. It
// refuses while a write in this process is between Checkpoint and Commit.
func (sm *SnapshotManager) undoNewest(check func(Entry) error, match func(Entry) bool, none error) (Entry, error) {
	if sm.dir == "" {
		return Entry{}, ErrDisabled
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.writeInProgressLocked() {
		return Entry{}, errWriteInProgress
	}
	unlock, err := lockSession(sm.dir, false)
	if err != nil {
		return Entry{}, err
	}
	defer unlock()
	sm.reloadLocked()
	for i := len(sm.entries) - 1; i >= 0; i-- {
		e := sm.entries[i]
		if !match(e) {
			continue
		}
		if check != nil {
			if err := check(e); err != nil {
				return e, err
			}
		}
		return e, sm.undoLocked(i)
	}
	return Entry{}, none
}

// RewindTo undoes, newest first, every entry from the first one recorded
// for messageID to the newest (W4's /rewind) and returns them in that
// order. On an error it stops and returns what it undid so far.
func (sm *SnapshotManager) RewindTo(messageID string) ([]Entry, error) {
	if messageID == "" {
		return nil, fmt.Errorf("no checkpoint for message %q in this session", messageID)
	}
	undone, found, err := sm.rewind([]string{messageID}, nil)
	if err == nil && !found {
		return nil, fmt.Errorf("no checkpoint for message %q in this session", messageID)
	}
	return undone, err
}

// RewindToAnyIf is RewindTo from the first entry recorded for any of ids
// (2.0 W4's /rewind, ruling 5). check (when not nil) first sees the
// entries it would undo, oldest first, under the store's locks; an error
// from it undoes nothing and is returned. When no entry has one of ids it
// undoes nothing and returns no error.
func (sm *SnapshotManager) RewindToAnyIf(ids []string, check func([]Entry) error) ([]Entry, error) {
	undone, _, err := sm.rewind(ids, check)
	return undone, err
}

func (sm *SnapshotManager) rewind(ids []string, check func([]Entry) error) (undone []Entry, found bool, err error) {
	if sm.dir == "" {
		return nil, false, ErrDisabled
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			want[id] = true
		}
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.writeInProgressLocked() {
		return nil, false, errWriteInProgress
	}
	unlock, err := lockSession(sm.dir, false)
	if err != nil {
		return nil, false, err
	}
	defer unlock()
	sm.reloadLocked()
	first := -1
	for i, e := range sm.entries {
		if want[e.MessageID] {
			first = i
			break
		}
	}
	if first < 0 {
		return nil, false, nil
	}
	if check != nil {
		if err := check(append([]Entry(nil), sm.entries[first:]...)); err != nil {
			return nil, true, err
		}
	}
	// Every backup must be there before anything is restored, so a
	// missing one stops the rewind before it starts rather than halfway.
	for _, e := range sm.entries[first:] {
		if e.Backup == "" {
			continue
		}
		p, err := sm.backupPath(e)
		if err == nil {
			_, err = os.Stat(p)
		}
		if err != nil {
			return nil, true, fmt.Errorf("cannot rewind: the backup of %s is unusable: %w", e.Path, err)
		}
	}
	// A restore that fails stops here: the entries undone so far are gone,
	// the failed one and older ones stay, so the same rewind again resumes.
	for i := len(sm.entries) - 1; i >= first; i-- {
		e := sm.entries[i]
		if err := sm.undoLocked(i); err != nil {
			return undone, true, err
		}
		undone = append(undone, e)
	}
	return undone, true, nil
}

// Evicted reports whether the cap evicted an entry recorded for any of ids
// (see evictedFile): a rewind of them cannot restore every change.
func (sm *SnapshotManager) Evicted(ids []string) bool {
	if sm.dir == "" || len(ids) == 0 {
		return false
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	gone := map[string]bool{}
	for _, id := range readEvicted(sm.dir) {
		gone[id] = true
	}
	for _, id := range ids {
		if id != "" && gone[id] {
			return true
		}
	}
	return false
}

// recordEvicted adds the call IDs of evicted entries to evictedFile.
// Callers hold sm.mu and the session lock.
func (sm *SnapshotManager) recordEvicted(evicted []Entry) error {
	var ids []string
	for _, e := range evicted {
		if e.MessageID != "" {
			ids = append(ids, e.MessageID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	all := append(readEvicted(sm.dir), ids...)
	if len(all) > maxEvictedIDs {
		all = all[len(all)-maxEvictedIDs:]
	}
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(sm.dir, evictedFile), append(data, '\n'), 0o600)
}

// readEvicted is dir's evictedFile; missing or unreadable is empty.
func readEvicted(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, evictedFile))
	if err != nil {
		return nil
	}
	var ids []string
	if json.Unmarshal(data, &ids) != nil {
		return nil
	}
	return ids
}

// SameEntry reports whether a and b are the same checkpoint.
func SameEntry(a, b Entry) bool { return sameEntry(a, b) }

// Files returns the sorted, de-duplicated paths of this session's entries:
// the files it changed (#200's files-modified list).
func (sm *SnapshotManager) Files() []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.reloadLocked()
	seen := map[string]bool{}
	var out []string
	for _, e := range sm.entries {
		if !seen[e.Path] {
			seen[e.Path] = true
			out = append(out, e.Path)
		}
	}
	sort.Strings(out)
	return out
}

// Entries returns a copy of the session's entries, oldest first.
func (sm *SnapshotManager) Entries() []Entry {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.reloadLocked()
	return append([]Entry(nil), sm.entries...)
}

// Cleanup removes the session's directory and forgets its entries.
func (sm *SnapshotManager) Cleanup() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.entries = nil
	if sm.dir != "" {
		return os.RemoveAll(sm.dir)
	}
	return nil
}

const (
	lockFile  = "index.lock"
	lockWait  = 10 * time.Second
	lockStale = 2 * time.Minute
	lockRetry = 10 * time.Millisecond
)

// lockSession serializes changes to dir's index across processes (two
// windows on one resumed session, celeste revert beside a chat): it
// creates dir/index.lock exclusively, waiting up to lockWait, and removes a
// lock older than lockStale (left by a process that died holding it; far
// longer than any checkpoint takes, so a live lock is never taken over). A
// session directory that does not exist has nothing to lock unless create.
// The returned function releases the lock.
func lockSession(dir string, create bool) (func(), error) {
	path := filepath.Join(dir, lockFile)
	token := newToken()
	deadline := time.Now().Add(lockWait)
	for {
		if create {
			// Every attempt: another process's refused first checkpoint
			// may have removed the directory meanwhile.
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("cannot create checkpoint directory: %w", err)
			}
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := f.WriteString(token)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("cannot lock checkpoint index in %s: %w", dir, werr)
			}
			return func() { releaseLock(path, token) }, nil
		}
		if !create && errors.Is(err, os.ErrNotExist) {
			if _, derr := os.Stat(dir); errors.Is(derr, os.ErrNotExist) {
				return func() {}, nil
			}
		}
		// Held by someone else, or (Windows) being deleted: wait.
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > lockStale {
			if held, rerr := os.ReadFile(path); rerr == nil {
				takeOverLock(path, string(held)) // then retry below
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("cannot lock checkpoint index in %s: %w", dir, err)
		}
		time.Sleep(lockRetry)
	}
}

// newToken identifies one lock holder.
func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d-%s", os.Getpid(), config.UniqueNanoID())
	}
	return hex.EncodeToString(b)
}

// releaseLock removes the lock at path only while it is still this
// holder's (a holder that was too slow may have lost it to a takeover).
func releaseLock(path, token string) {
	if held, err := os.ReadFile(path); err == nil && string(held) == token {
		_ = os.Remove(path)
	}
}

// takeOverLock removes the stale lock at path if it still holds token, the
// holder a waiter saw: it claims the file by renaming it (of two waiters,
// one rename wins) and gives it back when what it claimed is a newer
// holder's lock.
func takeOverLock(path, token string) bool {
	claim := path + ".claim-" + newToken()
	if err := os.Rename(path, claim); err != nil {
		return false
	}
	held, err := os.ReadFile(claim)
	if err == nil && string(held) == token {
		_ = os.Remove(claim)
		return true
	}
	_ = os.Rename(claim, path) // a newer holder's lock: put it back
	return false
}

// reloadLocked takes the index on disk as the truth: another process
// (celeste revert, a second window on the same session) may have changed
// it since this store last read or wrote it. An index that cannot be read
// leaves memory as it is; the next change rewrites it. Callers hold sm.mu.
func (sm *SnapshotManager) reloadLocked() {
	if entries, err := readIndex(sm.dir); err == nil {
		sm.entries = entries
	}
}

// undoLocked restores entry i's file, then removes the entry from the
// index and deletes its backup.
func (sm *SnapshotManager) undoLocked(i int) error {
	e := sm.entries[i]
	if err := sm.restore(e); err != nil {
		return err
	}
	next := append(append([]Entry(nil), sm.entries[:i]...), sm.entries[i+1:]...)
	if err := writeIndex(sm.dir, next); err != nil {
		return fmt.Errorf("cannot update checkpoint index: %w", err)
	}
	sm.entries = next
	sm.removeBackup(e)
	return nil
}

func (sm *SnapshotManager) restore(e Entry) error {
	ref, err := openRef(e)
	if err != nil {
		return fmt.Errorf("cannot restore %s: %w", e.Path, err)
	}
	defer ref.close()
	if e.Backup == "" {
		if err := ref.remove(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot remove %s: %w", e.Path, err)
		}
		return nil
	}
	src, err := sm.backupPath(e)
	if err != nil {
		return fmt.Errorf("cannot restore %s: %w", e.Path, err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("cannot restore %s: %w", e.Path, err)
	}
	cur, curErr := ref.readFile()
	if curErr == nil && bytes.Equal(cur, data) {
		// Already as checkpointed (a write that failed before changing a
		// byte, e.g. on a read-only file): nothing to restore.
		return nil
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(src); err == nil {
		perm = info.Mode().Perm()
	}
	// Atomic: a restore that fails halfway leaves the file as it was, not
	// truncated. The file keeps its current mode; a deleted one gets the
	// backup's. Being a new file renamed into place, it does not keep the
	// old file's other hard links, owner (when another user owned it),
	// extended attributes or ACLs. A symlink in its place is replaced, not
	// written through.
	err = atomicWrite(ref, data, perm)
	var noTemp *atomicfile.TempError
	if errors.As(err, &noTemp) && errors.Is(noTemp.Err, os.ErrPermission) {
		// The directory is not writable but the file may be (the write
		// tools change files in place): restore in place. Not atomic, so
		// only when the current contents could be read: a write that fails
		// halfway puts them back. Any other reason (a full disk) is
		// reported as it is.
		if _, serr := ref.stat(); serr == nil && curErr == nil {
			err = writeInPlace(ref, data, cur)
		}
	}
	if err != nil {
		return fmt.Errorf("cannot restore %s: %w", e.Path, err)
	}
	return nil
}

// atomicWrite is restore's atomic write (a test seam).
var atomicWrite = func(f fileRef, data []byte, perm os.FileMode) error { return f.replace(data, perm) }

// writeInPlace overwrites the existing file f with data, keeping the file
// itself (its mode, owner, links and attributes). prev is what the file
// holds now: when the write fails, it is written back (best effort), so a
// failed restore does not leave the file truncated.
func writeInPlace(f fileRef, data, prev []byte) error {
	err := overwrite(f, data)
	if err != nil {
		if rerr := overwrite(f, prev); rerr != nil {
			return fmt.Errorf("%w (and putting back its previous contents failed: %v)", err, rerr)
		}
	}
	return err
}

// overwrite truncates the file f and writes data to it.
func overwrite(f fileRef, data []byte) error {
	fh, err := f.open(os.O_WRONLY | os.O_TRUNC)
	if err != nil {
		return err
	}
	_, err = inPlaceWrite(fh, data)
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	return err
}

// inPlaceWrite is overwrite's write (a test seam).
var inPlaceWrite = func(f *os.File, b []byte) (int, error) { return f.Write(b) }

// backupPath is e's backup in the session directory. A name that is not a
// plain file name there (a hand-edited index) is refused, so no restore
// reads, and no cleanup deletes, a file outside the directory.
func (sm *SnapshotManager) backupPath(e Entry) (string, error) {
	b := e.Backup
	if b == "" || b != filepath.Base(b) || !filepath.IsLocal(b) || strings.EqualFold(b, indexFile) || strings.EqualFold(b, evictedFile) {
		return "", fmt.Errorf("invalid backup name %q in the checkpoint index", b)
	}
	return filepath.Join(sm.dir, b), nil
}

func (sm *SnapshotManager) removeBackup(e Entry) {
	if p, err := sm.backupPath(e); err == nil {
		_ = os.Remove(p)
	}
}

func (sm *SnapshotManager) nextVersionLocked(path string) int {
	maxV := 0
	for _, e := range sm.entries {
		if e.Path == path && e.Version > maxV {
			maxV = e.Version
		}
	}
	return maxV + 1
}

func sameEntry(a, b Entry) bool {
	return a.Path == b.Path && a.Version == b.Version && a.Backup == b.Backup &&
		a.MessageID == b.MessageID && a.Time.Equal(b.Time)
}

// readIndex loads dir's index; a missing index is an empty session.
func readIndex(dir string) ([]Entry, error) {
	if dir == "" {
		return nil, nil // checkpoints are off
	}
	data, err := os.ReadFile(filepath.Join(dir, indexFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// writeIndex replaces dir's index with entries, atomically: a reader in
// another process sees the old index or the new one, never half of one.
func writeIndex(dir string, entries []Entry) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if entries == nil {
		entries = []Entry{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, indexFile), append(data, '\n'), 0o600)
}

// backUp writes src's bytes to the backup dst, reading src again once if
// it changed during the read (its modification time moved). The backup is
// written atomically with src's mode plus owner write (so it can always be
// replaced and removed, also on Windows); a leftover file or symlink under
// its name is removed first, never written through.
func backUp(src, dst string, before os.FileInfo) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("snapshot copy failed: %w", err)
	}
	if info, err := os.Stat(src); err == nil && !info.ModTime().Equal(before.ModTime()) {
		if data, err = os.ReadFile(src); err != nil {
			return fmt.Errorf("snapshot retry copy failed: %w", err)
		}
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("snapshot copy failed: %w", err)
	}
	if err := atomicfile.Write(dst, data, before.Mode().Perm()|0o600); err != nil {
		return fmt.Errorf("snapshot copy failed: %w", err)
	}
	return nil
}

// maxBackupBase bounds the base-name part of a backup's name, so a long
// file name (plus atomicfile's temporary suffix) stays under the 255-byte
// name limit of common filesystems.
const maxBackupBase = 64

// backupName names version v of path's backup: 16 hex digits of the
// SHA-256 of the full path (distinct per path), the file's base name cut
// to maxBackupBase bytes on a rune boundary (for people reading the
// directory), and the version.
func backupName(path string, v int) string {
	sum := sha256.Sum256([]byte(path))
	base := filepath.Base(path)
	if len(base) > maxBackupBase {
		cut := maxBackupBase
		for cut > 0 && !utf8.RuneStart(base[cut]) {
			cut--
		}
		base = base[:cut]
	}
	return fmt.Sprintf("%s_%s_v%d", hex.EncodeToString(sum[:])[:16], base, v)
}
