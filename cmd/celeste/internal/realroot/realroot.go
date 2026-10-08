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
// say) is therefore checked by its absolute path instead: Lstat refuses
// it unless it is a directory, and the components after it are checked
// the same way until one can be opened, which is then compared with its
// Lstat before the walk goes on through descriptors. Only directories the
// process cannot read get the path check, and those are not its own.
func Open(dir string) (*os.Root, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	vol := filepath.VolumeName(dir)
	sep := string(filepath.Separator)
	prefix := vol + sep
	root, err := os.OpenRoot(prefix)
	if err != nil {
		if !errors.Is(err, fs.ErrPermission) {
			return nil, err
		}
		root = nil
	}
	for _, c := range strings.Split(dir[len(vol):], sep) {
		if c == "" {
			continue
		}
		path := filepath.Join(prefix, c)
		prefix = path
		if root != nil {
			next, err := openChild(root, c)
			_ = root.Close()
			root = nil
			if err == nil {
				root = next
				continue
			}
			if !errors.Is(err, fs.ErrPermission) {
				return nil, fmt.Errorf("%s: %w", dir, err)
			}
		}
		next, err := openPath(path)
		switch {
		case err == nil:
			root = next
		case errors.Is(err, fs.ErrPermission):
			// Searchable but not readable: go on by path.
		default:
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%s: %w", dir, fs.ErrPermission)
	}
	return root, nil
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

// openPath opens the directory at the absolute path, refusing a symlink
// or a directory swapped in after its Lstat. A directory that is a
// directory but cannot be read returns an fs.ErrPermission error.
func openPath(path string) (*os.Root, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", filepath.Base(path))
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return sameAs(r, before, filepath.Base(path))
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
