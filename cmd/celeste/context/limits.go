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
	// maxTotalSpillBytes caps every session's spill files together; the
	// startup prune removes the oldest sessions past it.
	maxTotalSpillBytes int64 = 1 << 30
)

// prunedSpillBases records the spill bases pruned in this process.
var prunedSpillBases sync.Map

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

	// Determine spill directory
	if baseDir == "" {
		baseDir, err = ToolResultsBaseDir()
		if err != nil {
			return result, false, err
		}
	}

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
	// session past its quota spills no more.
	saved := result
	if int64(len(saved)) > maxSpillFileBytes {
		saved = textutil.CutBytes(saved, int(maxSpillFileBytes))
	}
	// The file this write replaces (the same toolCallID spilled before)
	// does not count toward the quota.
	spillPath := filepath.Join(sessionDir, toolCallID+".txt")
	used := dirBytes(sessionDir)
	if fi, err := os.Lstat(spillPath); err == nil && fi.Mode().IsRegular() {
		used -= fi.Size()
	}
	if used+int64(len(saved)) > maxSessionSpillBytes {
		return result, false, fmt.Errorf("this session's spilled tool results reached %d bytes", maxSessionSpillBytes)
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
// ToolResultsBaseDir): every session directory last changed more than
// SpillKeepAge ago, then the oldest others while all of them together are
// over maxTotalSpillBytes. keep (the session running now) always survives.
// A missing baseDir is nothing to prune (Aikido 806869375).
func PruneToolResults(baseDir, keep string, now time.Time) error {
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
	for _, d := range des {
		if !d.IsDir() || d.Name() == keep {
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
	total += dirBytes(filepath.Join(baseDir, keep))
	sort.Slice(live, func(i, j int) bool { return live[i].changed.Before(live[j].changed) })
	for _, s := range live {
		if total <= maxTotalSpillBytes {
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
