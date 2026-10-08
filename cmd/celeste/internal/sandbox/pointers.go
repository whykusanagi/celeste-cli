package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// PathState is one watched path as SnapshotPaths saw it.
type PathState struct {
	Path    string
	Present bool        // anything is there
	Regular bool        // a regular file, whose content is Data
	Data    []byte      // a regular file's content, a symlink's target
	Mode    os.FileMode // the type and permission bits
}

// SnapshotPaths records each path: missing, a regular file and its
// content, a symlink and its target, or something else of a type.
func SnapshotPaths(paths []string) []PathState {
	out := make([]PathState, 0, len(paths))
	for _, p := range paths {
		s := PathState{Path: p}
		if info, err := os.Lstat(p); err == nil {
			s.Present, s.Mode = true, info.Mode()
			switch {
			case info.Mode().IsRegular():
				b, err := os.ReadFile(p)
				s.Regular, s.Data = err == nil, b
			case info.Mode()&os.ModeSymlink != 0:
				t, _ := os.Readlink(p)
				s.Data = []byte(t)
			}
		}
		out = append(out, s)
	}
	return out
}

// same reports whether two states of one path match.
func (s PathState) same(o PathState) bool {
	return s.Present == o.Present && s.Regular == o.Regular && s.Mode.Type() == o.Mode.Type() && bytes.Equal(s.Data, o.Data)
}

// RestorePaths puts every path in before back as it was: what appeared is
// removed, a regular file that changed or went away is rewritten. Only a
// regular file or nothing can be restored; anything else that changed is
// reported. It returns an error naming the paths it found changed, nil
// when none was. Run outside the sandbox after a sandboxed command
// (shellrun), it undoes a pointer the sandbox could not bind read-only
// because it did not exist yet (GitPointers): every process of a
// bubblewrap sandbox is gone by then, so none can plant it again.
func RestorePaths(before []PathState) error {
	var changed, failed []string
	for _, s := range before {
		now := SnapshotPaths([]string{s.Path})[0]
		if s.same(now) {
			continue
		}
		changed = append(changed, s.Path)
		if s.Present && !s.Regular {
			failed = append(failed, s.Path)
			continue
		}
		if now.Present {
			if err := os.RemoveAll(s.Path); err != nil {
				failed = append(failed, s.Path)
				continue
			}
		}
		if s.Regular {
			if err := os.WriteFile(s.Path, s.Data, s.Mode.Perm()); err != nil {
				failed = append(failed, s.Path)
			}
		}
	}
	switch {
	case len(failed) > 0:
		return fmt.Errorf("sandbox: the command changed git's %s, which tell git where its config is; celeste could not restore %s: check them before running git", strings.Join(changed, ", "), strings.Join(failed, ", "))
	case len(changed) > 0:
		return fmt.Errorf("sandbox: the command changed git's %s, which tell git where its config is; celeste restored them", strings.Join(changed, ", "))
	}
	return nil
}
