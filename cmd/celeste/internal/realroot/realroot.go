// Package realroot opens a directory as an os.Root without following a
// symlink anywhere on its path.
package realroot

import (
	"fmt"
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
func Open(dir string) (*os.Root, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	vol := filepath.VolumeName(dir)
	sep := string(filepath.Separator)
	root, err := os.OpenRoot(vol + sep)
	if err != nil {
		return nil, err
	}
	for _, c := range strings.Split(dir[len(vol):], sep) {
		if c == "" {
			continue
		}
		next, err := openChild(root, c)
		_ = root.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		root = next
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
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = child.Close()
		return nil, fmt.Errorf("%s changed while it was being opened", name)
	}
	return child, nil
}
