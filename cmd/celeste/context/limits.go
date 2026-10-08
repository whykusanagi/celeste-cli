package ctxmgr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/filelock"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/textutil"
)

const (
	// DefaultMaxToolResultBytes is the maximum size in bytes for a single tool
	// result before it gets capped and spilled to disk: 128 KiB.
	DefaultMaxToolResultBytes = 128 * 1024

	// SpillKeepAge is how long a session's spilled tool results are kept
	// after its last spill.
	SpillKeepAge = 30 * 24 * time.Hour
)

// Spill limits (Aikido 806869375). Vars so tests can lower them.
var (
	// maxSpillFileBytes caps one spill file: a larger result spills only
	// its first part.
	maxSpillFileBytes int64 = 32 << 20
	// maxSessionSpillBytes caps one session's spill files; past it a
	// result is cut in memory and not spilled.
	maxSessionSpillBytes int64 = 256 << 20
	// maxTotalSpillBytes caps every session's spill files together: a
	// spill that would pass it first removes the oldest other sessions
	// idle for spillActiveAge, and is cut in memory if that is not enough.
	maxTotalSpillBytes int64 = 1 << 30
)

// spillActiveAge is how long after its last spill a session may still be
// running in another celeste process: a prune for the total limit leaves
// such a session alone, so its recall_tool_result ids keep working.
const spillActiveAge = 15 * time.Minute

// testHookSpillChecked, when set, runs in CapToolResult after the quota
// checks pass, before the spill is written. Tests only.
var testHookSpillChecked func()

// prunedSpillBases records the spill bases pruned in this process.
var prunedSpillBases sync.Map

// spillMu serializes the quota checks and the write of each spill in this
// process; spillLockPath, a lock file beside the spill base, does the same
// across celeste processes.
var spillMu sync.Mutex

// compactStoreDir is the directory under the spill base where compaction
// keeps pruned tool-result bodies for recall_tool_result
// (compact.DefaultStore). It is not a spill session: the spill prunes and
// quotas leave it alone, and no session spills into it.
const compactStoreDir = "pruned"

// spillSessionName is the name of a session's spill directory. Only such a
// directory is pruned or counted toward the spill total; anything else
// under the base (the compaction store, a directory another tool made) is
// left alone.
var spillSessionName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// isSpillSession reports whether the directory name under the spill base
// is a spill session's.
func isSpillSession(name string) bool {
	return name != compactStoreDir && spillSessionName.MatchString(name)
}

// spillLockPath is the lock file of the spill base baseDir. It sits beside
// the base, not in it, so the base holds only session directories.
func spillLockPath(baseDir string) string { return filepath.Clean(baseDir) + ".lock" }

// ToolResultsBaseDir returns the base directory for spilled tool results.
// Default: ~/.celeste/tool-results
func ToolResultsBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".celeste", "tool-results"), nil
}

// CapToolResult checks whether a tool result exceeds maxBytes. If it does, the
// full result is written to disk at:
//
//	{baseDir}/{sessionID}/{toolCallID}.txt
//
// and SnipToolResult's head and tail of it are returned, around a marker that
// names the file.
//
// If baseDir is empty, ToolResultsBaseDir() is used.
//
// Returns:
//   - capped: the (possibly truncated) result string to send to the model
//   - wasCapped: true if the result was truncated
//   - err: any I/O error from writing the spill file
func CapToolResult(result string, maxBytes int, sessionID, toolCallID, baseDir string) (capped string, wasCapped bool, err error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxToolResultBytes
	}

	if len(result) <= maxBytes {
		return result, false, nil
	}

	// A session spills into a directory the prunes and quotas see: one
	// named like the compaction store gets its own name beside it, and a
	// name no session directory has is refused.
	if sessionID == compactStoreDir {
		sessionID = compactStoreDir + "-session"
	}
	if !isSpillSession(sessionID) {
		return result, false, fmt.Errorf("invalid spill session id %q", sessionID)
	}

	// Determine spill directory
	if baseDir == "" {
		baseDir, err = ToolResultsBaseDir()
		if err != nil {
			return result, false, err
		}
	}

	// The spill base itself is never pruned, so it is made before the lock,
	// whose file sits beside it.
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return result, false, fmt.Errorf("create tool-results dir: %w", err)
	}

	// Everything from the prune to the write happens under spillMu, and
	// across celeste processes under the lock file beside the base: two
	// spills at once cannot both pass a quota check only one of them fits
	// (Aikido review of #424), and another process's prune cannot remove
	// this session's directory between its creation and the write.
	spillMu.Lock()
	defer spillMu.Unlock()
	unlock, err := filelock.Lock(spillLockPath(baseDir), 10*time.Second)
	if err != nil {
		return result, false, fmt.Errorf("lock tool-results dir: %w", err)
	}
	defer unlock()

	// Once per process and base, before the first spill: drop old
	// sessions' spill files, so they do not pile up across sessions.
	if _, done := prunedSpillBases.LoadOrStore(baseDir, true); !done {
		_ = PruneToolResults(baseDir, sessionID, time.Now())
	}

	// 0700/0600: a spilled tool result may hold secrets (env dumps, API
	// responses, file contents), so keep it readable only by the owner.
	// MkdirAll/WriteFile only apply their mode to a path they create, so an
	// already-existing dir or file (an old binary's spill, or a reused
	// sessionID/toolCallID) is chmod'd explicitly too.
	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return result, false, fmt.Errorf("create tool-results dir: %w", err)
	}
	if err := os.Chmod(sessionDir, 0700); err != nil {
		return result, false, fmt.Errorf("secure tool-results dir: %w", err)
	}

	// A result over the per-file limit spills only its first part, and a
	// session past its quota spills no more. The file this write replaces
	// (the same toolCallID spilled before) does not count toward the
	// quotas.
	saved := result
	if int64(len(saved)) > maxSpillFileBytes {
		saved = textutil.CutBytes(saved, int(maxSpillFileBytes))
	}
	spillPath := filepath.Join(sessionDir, toolCallID+".txt")
	var replaced int64
	if fi, err := os.Lstat(spillPath); err == nil && fi.Mode().IsRegular() {
		replaced = fi.Size()
	}
	size := int64(len(saved))
	if dirBytes(sessionDir)-replaced+size > maxSessionSpillBytes {
		return result, false, fmt.Errorf("this session's spilled tool results reached %d bytes", maxSessionSpillBytes)
	}
	// Every spill keeps all sessions' spills within maxTotalSpillBytes,
	// not only the first of a run: past it, the oldest other sessions idle
	// for spillActiveAge go first (CodeRabbit review of #424), and if that
	// is not enough the result is cut in memory.
	if totalSpillBytes(baseDir)-replaced+size > maxTotalSpillBytes {
		_ = pruneToolResults(baseDir, sessionID, time.Now(), maxTotalSpillBytes-size+replaced)
		if totalSpillBytes(baseDir)-replaced+size > maxTotalSpillBytes {
			return result, false, fmt.Errorf("spilled tool results reached %d bytes in all", maxTotalSpillBytes)
		}
	}
	if testHookSpillChecked != nil {
		testHookSpillChecked()
	}

	if err := os.WriteFile(spillPath, []byte(saved), 0600); err != nil {
		return result, false, fmt.Errorf("write spill file: %w", err)
	}
	if err := os.Chmod(spillPath, 0600); err != nil {
		return result, false, fmt.Errorf("secure spill file: %w", err)
	}

	// The model sees the head and tail around a marker naming the spill
	// file (and, for a plain id, the recall_tool_result id).
	recall := ""
	if id := sessionID + "/" + toolCallID; spillIDPattern.MatchString(id) {
		// #211: the id recall_tool_result takes to page through the file.
		recall = fmt.Sprintf("; recall_tool_result with id %q returns it", id)
	}
	// The longest note that still fits beside the tail: a small cap or a
	// long spill path drops the path, then the recall id, rather than
	// overflow maxBytes.
	what := "full output"
	if len(saved) < len(result) {
		what = fmt.Sprintf("first %d bytes", len(saved))
	}
	notes := []string{
		fmt.Sprintf("TRUNCATED: %d bytes total, %s saved to: %s%s", len(result), what, spillPath, recall),
		fmt.Sprintf("TRUNCATED: %d bytes total, %s saved%s", len(result), what, recall),
		fmt.Sprintf("TRUNCATED: %d bytes total%s", len(result), recall),
		"TRUNCATED",
	}
	note := ""
	for _, n := range notes {
		if len(snipMarker(len(result), n))+min(256, maxBytes/4) <= maxBytes {
			note = n
			break
		}
	}
	return SnipToolResult(result, maxBytes, note), true, nil
}

// spillIDPattern is a spill file's recall id: <sessionID>/<toolCallID>, both
// plain names (the loop's are), so the id can never leave the spill base.
var spillIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}/[A-Za-z0-9_-]{1,128}$`)

// LoadSpilled returns the full tool result CapToolResult spilled under
// baseDir ("" is ToolResultsBaseDir) for a recall id <sessionID>/<toolCallID>,
// the id its notice names.
func LoadSpilled(baseDir, id string) (string, error) {
	if !spillIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid spilled tool result id %q", id)
	}
	if baseDir == "" {
		var err error
		if baseDir, err = ToolResultsBaseDir(); err != nil {
			return "", err
		}
	}
	session, name, _ := strings.Cut(id, "/")
	b, err := os.ReadFile(filepath.Join(baseDir, session, name+".txt"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no spilled tool result with id %q", id)
		}
		return "", err
	}
	return string(b), nil
}

// snipMarker marks where SnipToolResult cut a result; the note, when there
// is one, follows the count.
func snipMarker(snipped int, note string) string {
	if note == "" {
		return fmt.Sprintf("\n[...snipped %d bytes...]\n", snipped)
	}
	return fmt.Sprintf("\n[...snipped %d bytes. %s]\n", snipped, note)
}

// SnipToolResult cuts result to at most maxBytes in memory, keeping its head
// and tail around a "[...snipped N bytes...]" marker, without the spill file
// CapToolResult writes (2.0 F3). note, if not empty, goes into the marker:
// callers say there why the middle cannot be recalled and what to do instead.
// The loop uses it when the spill file cannot be written; sessions and
// checkpoints use it on tool results loaded from disk. Results at or under
// maxBytes come back unchanged. Cuts fall on UTF-8 character boundaries. The
// result is never over maxBytes: a cap too small for the marker gets a plain
// cut of the head.
func SnipToolResult(result string, maxBytes int, note string) string {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxToolResultBytes
	}
	if len(result) <= maxBytes {
		return result
	}
	// The snipped count is at most len(result), so this reserves enough for
	// the marker whatever the boundaries below turn out to be.
	reserve := len(snipMarker(len(result), note))
	tailLen := min(256, maxBytes/4)
	headLen := len(textutil.CutBytes(result, max(maxBytes-reserve-tailLen, 0)))
	tailStart := len(result) - tailLen
	for tailStart < len(result) && !utf8.RuneStart(result[tailStart]) {
		tailStart++
	}
	if out := result[:headLen] + snipMarker(tailStart-headLen, note) + result[tailStart:]; len(out) <= maxBytes {
		return out
	}
	return textutil.CutBytes(result, maxBytes)
}

// dirBytes is the total size of the regular files directly in dir.
func dirBytes(dir string) int64 {
	des, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var n int64
	for _, d := range des {
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
	}
	return n
}

// PruneToolResults deletes spilled tool results under baseDir ("" is
// ToolResultsBaseDir): only spill session directories (isSpillSession),
// never the compaction store beside them, whose bodies expire one by one
// after SpillKeepAge instead (expireCompactStore). Every session directory last changed more than
// SpillKeepAge ago, then the oldest others idle for spillActiveAge while all
// of them together are over maxTotalSpillBytes. keep (the session running
// now) always survives, and so does any session that spilled within
// spillActiveAge, as another celeste process may still be using it.
// A missing baseDir is nothing to prune (Aikido 806869375).
func PruneToolResults(baseDir, keep string, now time.Time) error {
	return pruneToolResults(baseDir, keep, now, maxTotalSpillBytes)
}

// pruneToolResults is PruneToolResults removing the oldest sessions while
// all of them together are over limit.
func pruneToolResults(baseDir, keep string, now time.Time, limit int64) error {
	if baseDir == "" {
		var err error
		if baseDir, err = ToolResultsBaseDir(); err != nil {
			return err
		}
	}
	des, err := os.ReadDir(baseDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type session struct {
		path    string
		changed time.Time
		size    int64
	}
	var live []session
	var total int64
	var errs []error
	if err := expireCompactStore(filepath.Join(baseDir, compactStoreDir), now); err != nil {
		errs = append(errs, err)
	}
	for _, d := range des {
		if !d.IsDir() || d.Name() == keep || !isSpillSession(d.Name()) {
			continue
		}
		path := filepath.Join(baseDir, d.Name())
		s := session{path: path, changed: lastSpill(path), size: dirBytes(path)}
		if now.Sub(s.changed) > SpillKeepAge {
			if err := os.RemoveAll(path); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		live = append(live, s)
		total += s.size
	}
	if isSpillSession(keep) {
		total += dirBytes(filepath.Join(baseDir, keep))
	}
	sort.Slice(live, func(i, j int) bool { return live[i].changed.Before(live[j].changed) })
	for _, s := range live {
		if total <= limit || now.Sub(s.changed) < spillActiveAge {
			break
		}
		if err := os.RemoveAll(s.path); err != nil {
			errs = append(errs, err)
			continue
		}
		total -= s.size
	}
	return errors.Join(errs...)
}

// expireCompactStore removes the compaction store's bodies (regular files
// directly in dir) last written more than SpillKeepAge ago, so the store,
// which the spill prunes and quotas leave alone, does not grow without
// bound. The store itself and its recent bodies stay.
func expireCompactStore(dir string, now time.Time) error {
	des, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var errs []error
	for _, d := range des {
		if !d.Type().IsRegular() {
			continue
		}
		info, err := d.Info()
		if err != nil || now.Sub(info.ModTime()) <= SpillKeepAge {
			continue
		}
		if err := os.Remove(filepath.Join(dir, d.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// totalSpillBytes is the size of every session's spill files under baseDir.
func totalSpillBytes(baseDir string) int64 {
	des, err := os.ReadDir(baseDir)
	if err != nil {
		return 0
	}
	var n int64
	for _, d := range des {
		if d.IsDir() && isSpillSession(d.Name()) {
			n += dirBytes(filepath.Join(baseDir, d.Name()))
		}
	}
	return n
}

// lastSpill is when a session directory last changed: its newest file's
// modification time, or the directory's own.
func lastSpill(dir string) time.Time {
	var t time.Time
	if info, err := os.Stat(dir); err == nil {
		t = info.ModTime()
	}
	des, _ := os.ReadDir(dir)
	for _, d := range des {
		if info, err := d.Info(); err == nil && info.ModTime().After(t) {
			t = info.ModTime()
		}
	}
	return t
}
