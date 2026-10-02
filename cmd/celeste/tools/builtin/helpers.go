package builtin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/atomicfile"
)

// resolvePath checks that the resolved absolute path stays within the workspace.
func resolvePath(workspace, input string, forWrite bool) (string, error) {
	path, _, err := resolvePathReal(workspace, input, forWrite)
	return path, err
}

// resolvePathReal is resolvePath that also returns realPath, the
// symlink-resolved path it checked. The write tools write to it (2.0 W4):
// atomicWrite's rename lands on the checked file, never through a symlink
// swapped in after the check.
func resolvePathReal(workspace, input string, forWrite bool) (path, realPath string, err error) {
	workspace = filepath.Clean(workspace)
	if input == "" {
		input = "."
	}

	var candidate string
	if filepath.IsAbs(input) {
		candidate = filepath.Clean(input)
	} else {
		candidate = filepath.Clean(filepath.Join(workspace, input))
	}

	if !withinDir(workspace, candidate) {
		return "", "", fmt.Errorf("path escapes workspace: %s", input)
	}

	// The check above is lexical, so a symlink inside the workspace that
	// points outside it would pass. Resolve symlinks on both sides and check
	// again (#187).
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		realWorkspace = workspace
	}
	realCandidate, err := resolveExisting(candidate)
	if err != nil {
		return "", "", fmt.Errorf("resolve %s: %w", input, err)
	}
	if !withinDir(realWorkspace, realCandidate) {
		return "", "", fmt.Errorf("path escapes workspace through a symlink: %s", input)
	}
	if forWrite {
		if reason := protectedHookFile(candidate, realCandidate); reason != "" {
			return "", "", fmt.Errorf("%s", reason)
		}
	}
	return candidate, realCandidate, nil
}

// withinDir reports whether path is dir or lies under it (both cleaned).
func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// resolveExisting resolves symlinks in the longest existing prefix of path and
// re-attaches the rest, so a file that doesn't exist yet can still be checked.
// A dangling symlink is an error: writing through it would create its target,
// wherever that is.
func resolveExisting(path string) (string, error) {
	rest := ""
	cur := path
	for {
		if _, err := os.Lstat(cur); err == nil {
			real, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			return filepath.Join(real, rest), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func getStringArg(args map[string]any, key, fallback string) string {
	if v, ok := args[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		case fmt.Stringer:
			return s.String()
		}
	}
	return fallback
}

func getBoolArg(args map[string]any, key string, fallback bool) bool {
	if v, ok := args[key]; ok {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(b))
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func getIntArg(args map[string]any, key string, fallback int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int32:
			return int(n)
		case int64:
			return int(n)
		case float32:
			return int(n)
		case float64:
			return int(n)
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(n))
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func fileSize(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}

// atomicWrite replaces path through a temp file in its directory and a
// rename (2.0 W4 ruling 5): a reader never sees a half-written file, an
// existing file keeps its mode, a new one gets perm. path is the
// symlink-resolved path resolvePathReal checked; a symlink found there now
// is replaced, not followed. A file with several hard links is rewritten
// in place instead, as editors do: a rename would leave its other names
// holding the old content.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	if fi, err := os.Lstat(path); err == nil && hardLinked(path, fi) {
		f, err := openInPlace(path)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}
	return atomicfile.ReplaceKeepMode(path, data, perm)
}

// writeFileFunc writes a whole file for write_file, patch_file and
// splice_file. Tests replace it to make a write fail after its checkpoint
// (2.0 F4); only serial tests may, restoring it with t.Cleanup.
var writeFileFunc = atomicWrite

// notReadMessage is the must-read refusal, naming the path as the model
// wrote it (2.0 W4 ruling 6).
func notReadMessage(path string) string {
	return fmt.Sprintf("read_file %s first: celeste edits an existing file only after reading it in this session", path)
}

// checkRead applies the must-read-before-edit rule for target (the path
// the model gave is shown); "" when the edit may go ahead.
func checkRead(ft *checkpoints.FileTracker, target, shown string) string {
	if ft == nil {
		return ""
	}
	err := ft.CheckRead(target)
	if errors.Is(err, checkpoints.ErrNotRead) {
		return notReadMessage(shown)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// commit records, after a call's writes succeeded, the state each file
// was left in (checkpoints.Entry.After), closing its checkpoints. A commit
// that fails leaves the entry without it, so undoing it warns.
func commit(ckpts ...*checkpoints.Checkpoint) {
	for _, c := range ckpts {
		if c != nil {
			_ = c.Commit()
		}
	}
}

// rollback undoes the checkpoints a failed call took, newest first, and
// returns msg with any rollback failure appended.
func rollback(msg string, ckpts ...*checkpoints.Checkpoint) string {
	for i := len(ckpts) - 1; i >= 0; i-- {
		if ckpts[i] == nil {
			continue
		}
		if err := ckpts[i].Rollback(); err != nil {
			msg += fmt.Sprintf("; restoring %s failed: %v", ckpts[i].Entry().Path, err)
		}
	}
	return msg
}
