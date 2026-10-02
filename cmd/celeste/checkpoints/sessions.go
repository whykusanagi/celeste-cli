package checkpoints

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Root is where every session's checkpoints live: ~/.celeste/checkpoints.
func Root() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".celeste", "checkpoints")
}

// SessionDir is sessionID's directory under root (ruling 5).
func SessionDir(root, sessionID string) string {
	return filepath.Join(root, safeName(sessionID))
}

// safeName keeps [A-Za-z0-9._-] and replaces every other byte with '_'.
// "", "." and ".." get a leading '_' so they never name root or its parent.
func safeName(id string) string {
	b := []byte(id)
	for i, c := range b {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			b[i] = '_'
		}
	}
	s := string(b)
	if s == "" || s == "." || s == ".." {
		s = "_" + s
	}
	return s
}

// Retention (2.0 F4): a session's checkpoints survive while the session is
// one of the KeepSessions most recently changed, or was changed less than
// KeepAge ago — whichever keeps more.
const (
	KeepSessions = 20
	KeepAge      = 30 * 24 * time.Hour
)

// Prune deletes the session directories under root that retention does
// not keep. keep (the session starting now) always survives. A missing
// root is nothing to prune.
func Prune(root, keep string, now time.Time) error {
	des, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type session struct {
		name    string
		changed time.Time
	}
	var all []session
	for _, d := range des {
		if d.IsDir() {
			all = append(all, session{d.Name(), lastChange(filepath.Join(root, d.Name()))})
		}
	}
	// The session starting now is the most recent: it takes one of the
	// KeepSessions places, however old its last change.
	current := safeName(keep)
	sort.Slice(all, func(i, j int) bool {
		if (all[i].name == current) != (all[j].name == current) {
			return all[i].name == current
		}
		return all[i].changed.After(all[j].changed)
	})
	var errs []error
	for i, s := range all {
		if i < KeepSessions || now.Sub(s.changed) < KeepAge || s.name == current {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, s.name)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// lastChange is when a session last changed: its index's modification
// time, or the directory's for a 1.x checkpoint directory with no index.
func lastChange(dir string) time.Time {
	if info, err := os.Stat(filepath.Join(dir, indexFile)); err == nil {
		return info.ModTime()
	}
	if info, err := os.Stat(dir); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// LatestSessionFor returns the session (its directory name under root)
// whose newest checkpoint of path is the most recent across all sessions
// (celeste revert without --session). Paths match as samePath does.
func LatestSessionFor(root, path string) (string, error) {
	des, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var best string
	var bestTime time.Time
	for _, d := range des {
		if !d.IsDir() {
			continue
		}
		entries, err := readIndex(filepath.Join(root, d.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if samePath(e.Path, path) && (best == "" || e.Time.After(bestTime)) {
				best, bestTime = d.Name(), e.Time
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no checkpoint of %s in any session", path)
	}
	return best, nil
}

// RevertFile restores path from its newest checkpoint in sessionID — or,
// when sessionID is "", in the session that changed it last — and removes
// that entry (celeste revert, 2.0 F4).
func RevertFile(root, path, sessionID string) (string, Entry, error) {
	if sessionID == "" {
		latest, err := LatestSessionFor(root, path)
		if err != nil {
			return "", Entry{}, err
		}
		sessionID = latest
	}
	dir := SessionDir(root, sessionID)
	if _, err := os.Stat(dir); err != nil {
		return sessionID, Entry{}, fmt.Errorf("no checkpoints for session %s", sessionID)
	}
	e, err := newSnapshotManagerWithBase(dir).Revert(path)
	if errors.Is(err, errNoCheckpoint) {
		return sessionID, Entry{}, fmt.Errorf("no checkpoint of %s in session %s", path, sessionID)
	}
	if err != nil {
		return sessionID, Entry{}, err
	}
	return sessionID, e, nil
}
