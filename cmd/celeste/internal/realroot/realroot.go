// Package realroot opens a directory as an os.Root without following a
// symlink anywhere on its path.
package realroot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Open opens dir (made absolute against the current directory) as an
// os.Root. It starts at the volume
// root and opens each component of dir through the directory opened
// before it, refusing a component that is not a directory (a symlink
// among them) and checking on the opened descriptor that it is the
// directory Lstat saw. A directory on the path replaced by a symlink
// between a caller's check of dir and this open is refused instead of
// followed, which os.OpenRoot(dir) alone would do.
//
// Opening a directory needs read permission on it, but walking a path
// needs only search permission. A directory on the path that can be
// entered but not read (a parent owned by another account with mode 0711,
// say) cannot be opened, so the walk goes on below it by path until a
// component can be opened again. Such a path could follow a symlink
// swapped in for the unreadable directory (by a process that can write
// the directory holding it), so the directory opened there is checked
// against its real ancestry, read through ".." on its own descriptor:
// each parent up to the last directory the walk did open must be the one
// it checked, the first of them through that directory's descriptor. A
// swap fails the open instead of being followed. Where that check is not
// available (Windows) an unreadable directory on the path fails the open.
func Open(dir string) (*os.Root, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	vol := filepath.VolumeName(dir)
	sep := string(filepath.Separator)
	prefix := vol + sep
	// root is the directory opened last; while the walk is below an
	// unreadable directory it is nil, anchor is the last directory opened
	// (nil when that is the volume root, which could not be opened) and
	// pending holds what each unreadable directory since it was.
	var anchor *os.Root
	var anchorInfo fs.FileInfo
	var pending []fs.FileInfo
	closeAnchor := func() {
		if anchor != nil {
			_ = anchor.Close()
			anchor = nil
		}
	}
	root, err := os.OpenRoot(prefix)
	if err != nil {
		if !errors.Is(err, fs.ErrPermission) || !ancestryCheckable {
			return nil, err
		}
		root = nil
		if anchorInfo, err = os.Lstat(prefix); err != nil {
			return nil, err
		}
	}
	for _, c := range strings.Split(dir[len(vol):], sep) {
		if c == "" {
			continue
		}
		path := filepath.Join(prefix, c)
		prefix = path
		if root != nil {
			next, err := openChild(root, c)
			if err == nil {
				_ = root.Close()
				root = next
				continue
			}
			if !errors.Is(err, fs.ErrPermission) || !ancestryCheckable {
				_ = root.Close()
				return nil, fmt.Errorf("%s: %w", dir, err)
			}
			// Searchable but not readable: remember it as seen through
			// root, and go on below it by path.
			anchor, root = root, nil
			if anchorInfo, err = anchor.Stat("."); err != nil {
				closeAnchor()
				return nil, fmt.Errorf("%s: %w", dir, err)
			}
			info, err := anchor.Lstat(c)
			if err != nil {
				closeAnchor()
				return nil, fmt.Errorf("%s: %w", dir, err)
			}
			pending = []fs.FileInfo{info}
			continue
		}
		if testHookPathStep != nil {
			testHookPathStep(path)
		}
		info, next, err := openBelow(path, anchorInfo, pending)
		switch {
		case err == nil:
			closeAnchor()
			root, pending = next, nil
		case errors.Is(err, fs.ErrPermission) && info != nil:
			// Searchable but not readable: go on by path.
			pending = append(pending, info)
		default:
			closeAnchor()
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
	}
	closeAnchor()
	if root == nil {
		return nil, fmt.Errorf("%s: %w", dir, fs.ErrPermission)
	}
	return root, nil
}

// openBelow opens the directory at the absolute path, reached by path
// below the unreadable directories pending (each a child of the one
// before, the first a child of the directory anchor describes). It
// refuses a symlink, and a directory whose real ancestors are not pending
// and anchor. A directory that cannot be read returns its Lstat and an
// fs.ErrPermission error.
func openBelow(path string, anchor fs.FileInfo, pending []fs.FileInfo) (fs.FileInfo, *os.Root, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !before.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", filepath.Base(path))
	}
	f, err := openDirNoFollow(path)
	if err != nil {
		return before, nil, err
	}
	defer f.Close()
	got, err := f.Stat()
	if err != nil || !os.SameFile(before, got) {
		return nil, nil, fmt.Errorf("%s changed while it was being opened", filepath.Base(path))
	}
	// want is the expected ancestry, outermost first: the parent of f is
	// its last entry.
	want := append([]fs.FileInfo{anchor}, pending...)
	for up := 1; up <= len(want); up++ {
		ok, err := ancestorIs(f, up, want[len(want)-up])
		if err != nil || !ok {
			return nil, nil, fmt.Errorf("%s changed while it was being opened", filepath.Base(path))
		}
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	r, err = sameAs(r, got, filepath.Base(path))
	return nil, r, err
}

// openChild opens the directory name inside parent, refusing a symlink
// or a directory swapped in after its Lstat.
func openChild(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", name)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return sameAs(child, before, name)
}

// sameAs returns r when it is the directory before describes, and closes
// it otherwise.
func sameAs(r *os.Root, before fs.FileInfo, name string) (*os.Root, error) {
	after, err := r.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = r.Close()
		return nil, fmt.Errorf("%s changed while it was being opened", name)
	}
	return r, nil
}

var testHookPathStep func(path string)
